package config

import (
	"log/slog"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server      *ServerConfig    `mapstructure:"server"`
	BackEnds    []*BackEndConfig `mapstructure:"backends"`
	Strategy    *StrategyConfig  `mapstructure:"load_balancer"`
	LogConfig   *LogConfig       `mapstructure:"log"`
	RedisConfig *CacheConfig     `mapstructure:"cache"`
}

type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

type BackEndConfig struct {
	Url    string `mapstructure:"url"`
	Weight int    `mapstructure:"weight"`
}

type ServerConfig struct {
	Port                int    `mapstructure:"port"`
	HealthCheckInterval string `mapstructure:"health_check_interval"`
}

type StrategyConfig struct {
	Strategy string `mapstructure:"strategy"`
}

type CacheConfig struct {
	Addr     string        `mapstructure:"addr"`
	Password string        `mapstructure:"password"`
	DB       int           `mapstructure:"db"`
	PoolSize int           `mapstructure:"pool_size"`
	Timeout  time.Duration `mapstructure:"timeout"`
}

type RouteRule struct {
	Prefix      string `mapstructure:"prefix"`
	Service     string `mapstructure:"service_name"`
	StripPrefix bool   `mapstructure:"strip_prefix,omitempty"`
}

type RoutingConfig struct {
	Rules          []RouteRule `mapstructure:"rules"`
	DefaultService string      `mapstructure:"default_service,omitempty"`
}

type RetryConfig struct {
	MaxRetries   int           `mapstructure:"retry:max_retries"`
	BaseDelay    time.Duration `mapstructure:"retry:base_delay"`
	MaxDelay     time.Duration `mapstructure:"retry:max_delay"`
	JitterFactor float64       `mapstructure:"retry:jitter_factor"`
}

func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries:   3,
		BaseDelay:    200 * time.Millisecond,
		MaxDelay:     3 * time.Second,
		JitterFactor: 0.2,
	}
}

type RateLimitConfig struct {
	Enabled           bool          `mapstructure:"enabled"`
	RequestsPerSecond float64       `mapstructure:"requests_per_second"`
	Burst             int           `mapstructure:"burst"`
	CleanupInterval   time.Duration `mapstructure:"cleanup_interval_seconds"` // viper tự convert seconds → Duration
	CleanupTTL        time.Duration `mapstructure:"cleanup_ttl_minutes"`      // viper tự convert minutes → Duration
	TrustedProxies    []string      `mapstructure:"trusted_proxies"`
}

func DefaultRateLimitConfig() *RateLimitConfig {
	return &RateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10.0,
		Burst:             20,
		CleanupInterval:   60 * time.Second,
		CleanupTTL:        3 * time.Minute,
		TrustedProxies:    []string{},
	}
}

type StickySessionConfig struct {
	CookieName    string        `mapstructure:"cookie_name"`
	TTL           time.Duration `mapstructure:"ttl_seconds"`
	Secure        bool          `mapstructure:"secure"`
	EncryptionKey []byte
}

type stickyInternal struct {
	CookieName    string        `mapstructure:"cookie_name"`
	TTL           time.Duration `mapstructure:"ttl_seconds"`
	Secure        bool          `mapstructure:"secure"`
	EncryptionKey string        `mapstructure:"encryption_key"`
}

func DefaultStickySessionConfig() *StickySessionConfig {
	return &StickySessionConfig{
		CookieName:    "lb_sid",
		TTL:           3600 * time.Second,
		Secure:        true,
		EncryptionKey: []byte("passphrasewith32bytescharacters!"),
	}
}

type CircuitBreakerConfig struct {
	MaxConsecutiveFailures uint32        `mapstructure:"max_consecutive_failures"`
	Timeout                time.Duration `mapstructure:"timeout_seconds"`
	Interval               time.Duration `mapstructure:"interval_seconds"`
}

func DefaultCircuitBreakerConfig() *CircuitBreakerConfig {
	return &CircuitBreakerConfig{
		MaxConsecutiveFailures: 3,
		Timeout:                5 * time.Second,
		Interval:               10 * time.Second,
	}
}

type resultConfig struct {
	config         *Config
	router         *RoutingConfig
	retry          *RetryConfig
	ratelimit      *RateLimitConfig
	circuitBreaker *CircuitBreakerConfig
	sticky         *StickySessionConfig
}

func unMarshalConfig(v *viper.Viper) (*resultConfig, error) {
	var cfg *Config
	var routing *RoutingConfig
	var retry *RetryConfig
	var rateLimit *RateLimitConfig
	var circuitBreaker *CircuitBreakerConfig
	var sticky *StickySessionConfig

	if err := v.Unmarshal(&cfg); err != nil {
		slog.Error("Failed to unmarshall config")
		return nil, err
	}

	if err := v.Unmarshal(&routing); err != nil {
		slog.Error("Failed to unmarshall routing")
		return nil, err
	}

	stickySub := v.Sub("sticky_session")
	if stickySub != nil {
		var si stickyInternal
		if err := stickySub.Unmarshal(&si); err != nil {
			return nil, err
		}
		sticky = &StickySessionConfig{
			CookieName:    si.CookieName,
			TTL:           si.TTL,
			Secure:        si.Secure,
			EncryptionKey: []byte(si.EncryptionKey),
		}

		if sticky.TTL > 0 && sticky.TTL < time.Second {
			sticky.TTL = sticky.TTL * time.Second
		}
	} else {
		slog.Info("Use default sticky config")
		sticky = DefaultStickySessionConfig()
	}

	retrySub := v.Sub("retry")
	if retrySub != nil {
		if err := retrySub.Unmarshal(&retry); err != nil {
			slog.Warn("Failed to unmarshall retry")
			return nil, err
		}
	} else {
		slog.Info("use default retry config")
		retry = DefaultRetryConfig()
	}

	rateLimitSub := v.Sub("rate_limit")
	if rateLimitSub != nil {
		if err := rateLimitSub.Unmarshal(&rateLimit); err != nil {
			slog.Warn("Failed to unmarshall rate limit")
			return nil, err
		}
	} else {
		slog.Info("Use default rate limit")
		rateLimit = DefaultRateLimitConfig()
	}

	circuitBreakerSub := v.Sub("circuit_breaker")
	if circuitBreakerSub != nil {
		if err := circuitBreakerSub.Unmarshal(&circuitBreaker); err != nil {
			slog.Warn("Failed to unmarshall circuit breaker")
			return nil, err
		}
	} else {
		slog.Info("Use default circuit breaker")
		circuitBreaker = DefaultCircuitBreakerConfig()
	}

	// cleanup_interval_seconds và cleanup_ttl_minutes là số nguyên trong yaml
	// viper unmarshal thành int64 nanoseconds nếu dùng time.Duration trực tiếp — cần convert
	if rateLimit.CleanupInterval == 0 {
		rateLimit.CleanupInterval = DefaultRateLimitConfig().CleanupInterval
	} else if rateLimit.CleanupInterval < time.Second {
		// viper đọc số nguyên (60) thành 60 nanoseconds — cần convert sang seconds
		rateLimit.CleanupInterval = rateLimit.CleanupInterval * time.Second
	}

	if rateLimit.CleanupTTL == 0 {
		rateLimit.CleanupTTL = DefaultRateLimitConfig().CleanupTTL
	} else if rateLimit.CleanupTTL < time.Minute {
		rateLimit.CleanupTTL = rateLimit.CleanupTTL * time.Minute
	}
	result := &resultConfig{
		config:         cfg,
		router:         routing,
		retry:          retry,
		ratelimit:      rateLimit,
		circuitBreaker: circuitBreaker,
		sticky:         sticky,
	}
	return result, nil
}
