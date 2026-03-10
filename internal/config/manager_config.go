package config

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/viper"
)

var (
	once     sync.Once
	instance *slog.Logger
)

type ConfigSnapshot struct {
	Config         *Config
	Routing        *RoutingConfig
	Retry          *RetryConfig
	RateLimit      *RateLimitConfig
	StickySession  *StickySessionConfig
	CircuitBreaker *CircuitBreakerConfig
	LastReload     time.Time
}

type ConfigManager struct {
	snapshot     atomic.Pointer[ConfigSnapshot]
	reloadErrors int
	viper        *viper.Viper
	onChange     func(*Config)
	mux          sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	startOne     sync.Once
	stopOne      sync.Once
	wg           sync.WaitGroup
	firstInitial bool
	logger       *slog.Logger
}

func NewConfigManager(configDir string, onChange func(*Config)) (*ConfigManager, error) {
	m := &ConfigManager{
		onChange:     onChange,
		firstInitial: true,
		logger:       getDefaultLogger(),
	}

	m.loadConfig(configDir)

	m.logger.Info("Config manager initialized with hot-reload")

	return m, nil
}

func (m *ConfigManager) Start() error {
	var err error
	m.startOne.Do(func() {
		m.ctx, m.cancel = context.WithCancel(context.Background())
		m.wg.Add(1)

		go m.watchConfigWithDebounce()
		m.logger.Info("Starting Watcher config file")
	})

	return err
}

func (m *ConfigManager) Stop() {
	m.stopOne.Do(func() {
		if m.cancel != nil {
			m.logger.Info("Stopping watcher config manager")
			m.cancel()
		}
		if m.viper != nil {
			m.wg.Wait()
		}
		m.logger.Info("Complete stop watcher config manger")
	})
}

func (m *ConfigManager) StoreSnapshot(s *ConfigSnapshot) {
	m.snapshot.Store(s)
}

func (m *ConfigManager) GetSnapshot() *ConfigSnapshot {
	return m.snapshot.Load()
}

func (m *ConfigManager) GetStickySessionConfig() *StickySessionConfig {
	return m.snapshot.Load().StickySession
}

func (m *ConfigManager) GetRateLimitConfig() *RateLimitConfig {
	if s := m.snapshot.Load(); s != nil && s.RateLimit != nil {
		return s.RateLimit
	}
	return DefaultRateLimitConfig()
}

func (m *ConfigManager) GetCircuitBreakerConfig() *CircuitBreakerConfig {
	if s := m.snapshot.Load(); s != nil && s.CircuitBreaker != nil {
		return s.CircuitBreaker
	}
	return DefaultCircuitBreakerConfig()
}

func (m *ConfigManager) GetConfig() *Config {
	if s := m.snapshot.Load(); s != nil {
		return s.Config
	}
	return nil
}

func (m *ConfigManager) GetRoutingConfig() *RoutingConfig {
	if s := m.snapshot.Load(); s != nil {
		return s.Routing
	}
	return nil
}

func (m *ConfigManager) GetRetryConfig() *RetryConfig {
	if s := m.snapshot.Load(); s != nil {
		return s.Retry
	}
	return nil
}

func (m *ConfigManager) SetOnChange(fn func(*Config)) {
	m.mux.Lock()
	m.onChange = fn
	m.mux.Unlock()
}

func (m *ConfigManager) GetHealthCheckInterval() time.Duration {
	s := m.snapshot.Load()
	if s == nil || s.Config == nil || s.Config.Server == nil {
		return 10 * time.Second
	}
	d, err := time.ParseDuration(s.Config.Server.HealthCheckInterval)
	if err != nil {
		m.logger.Error("parse health interval failed", "err", err)
		return 10 * time.Second
	}
	return d
}

func getDefaultLogger() *slog.Logger {
	once.Do(func() {
		level := slog.LevelDebug

		opts := &slog.HandlerOptions{
			Level:     level,
			AddSource: level == slog.LevelDebug,
		}

		rootDir := getRootDir()
		logPath := filepath.Join(rootDir, "app.log")

		logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
		var output io.Writer = os.Stdout

		if err == nil {
			output = io.MultiWriter(os.Stdout, logFile)
		} else {
			fmt.Printf("Warning: Could not open log file, logging to stdout only: %v\n", err)
		}

		var handler slog.Handler
		handler = slog.NewTextHandler(output, opts)

		instance = slog.New(handler).With(
			slog.String("service", "load balancer"),
		)
	})

	return instance
}

func getRootDir() string {
	_, filename, _, ok := runtime.Caller(0)
	if ok {
		return filepath.Dir(filepath.Dir(filepath.Dir(filename)))
	}

	exePath, err := os.Executable()
	if err != nil {
		dir, _ := os.Getwd()
		return dir
	}

	return filepath.Dir(exePath)
}
