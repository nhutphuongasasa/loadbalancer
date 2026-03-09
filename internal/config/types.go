package config

import (
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
	MaxRetries   int           `mapstructure:"max_retries"`
	BaseDelay    time.Duration `mapstructure:"base_delay"`
	MaxDelay     time.Duration `mapstructure:"max_delay"`
	JitterFactor float64       `mapstructure:"jitter_factor"`
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

func unMarshalConfig(v *viper.Viper) (*Config, *RoutingConfig, *RetryConfig, *RateLimitConfig, error) {
	var cfg Config
	var routing RoutingConfig
	var retry RetryConfig
	var rateLimit RateLimitConfig

	if err := v.Unmarshal(&cfg); err != nil {
		return nil, nil, nil, nil, err
	}

	if err := v.UnmarshalKey("routing", &routing); err != nil {
		return nil, nil, nil, nil, err
	}

	if err := v.UnmarshalKey("retry", &retry); err != nil {
		return nil, nil, nil, nil, err
	}

	if err := v.UnmarshalKey("rate_limit", &rateLimit); err != nil {
		return nil, nil, nil, nil, err
	}

	// viper đọc số nguyên (60) thành 60 nanoseconds — cần convert sang seconds
	if rateLimit.CleanupInterval == 0 {
		rateLimit.CleanupInterval = DefaultRateLimitConfig().CleanupInterval
	} else if rateLimit.CleanupInterval < time.Second {
		rateLimit.CleanupInterval = rateLimit.CleanupInterval * time.Second
	}

	if rateLimit.CleanupTTL == 0 {
		rateLimit.CleanupTTL = DefaultRateLimitConfig().CleanupTTL
	} else if rateLimit.CleanupTTL < time.Minute {
		rateLimit.CleanupTTL = rateLimit.CleanupTTL * time.Minute
	}

	return &cfg, &routing, &retry, &rateLimit, nil
}
