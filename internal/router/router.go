package router

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"log/slog"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

type RouteRule struct {
	Prefix      string `mapstructure:"prefix"`
	Service     string `mapstructure:"service_name"`
	StripPrefix bool   `mapstructure:"strip_prefix,omitempty"`
}

type RoutingConfig struct {
	Rules          []RouteRule `mapstructure:"rules"`
	DefaultService string      `mapstructure:"default_service,omitempty"`
}

type PathRouter struct {
	mu         sync.RWMutex
	rules      []RouteRule
	defaultSvc string
	logger     *slog.Logger
	viper      *viper.Viper
	configPath string
}

func NewPathRouter(configPath string, logger *slog.Logger) (*PathRouter, error) {
	if logger == nil {
		logger = slog.Default()
	}

	configPath = filepath.Join(configPath, "config")

	pr := &PathRouter{
		logger:     logger,
		configPath: configPath,
	}

	v := viper.New()
	v.SetConfigName("routing")
	v.SetConfigType("yml")
	v.AddConfigPath(configPath)

	v.AutomaticEnv()
	v.SetEnvPrefix("app")
	v.BindEnv("default_service", "APP_DEFAULT_SERVICE")

	pr.viper = v

	if err := pr.reloadConfig(); err != nil {
		return nil, err
	}

	go pr.watchConfig()

	pr.logger.Info("PathRouter initialized", "config_path", configPath)

	return pr, nil
}

func (pr *PathRouter) reloadConfig() error {
	if err := pr.viper.ReadInConfig(); err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	var cfg RoutingConfig
	if err := pr.viper.Unmarshal(&cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	valid, valErr := pr.validateRoutingConfig(&cfg)
	if !valid {
		pr.logger.Error("Invalid routing config, keeping previous configuration",
			slog.Any("validation_errors", valErr),
			slog.String("file", pr.viper.ConfigFileUsed()),
		)
		return nil
	}

	pr.mu.Lock()
	pr.rules = cfg.Rules
	pr.defaultSvc = cfg.DefaultService
	pr.mu.Unlock()

	pr.logger.Info("Routing config reloaded and validated successfully",
		slog.Int("rules_count", len(pr.rules)),
		slog.String("default_svc", pr.defaultSvc),
	)

	return nil
}

func (pr *PathRouter) watchConfig() {
	pr.viper.WatchConfig()
	pr.viper.OnConfigChange(func(e fsnotify.Event) {
		pr.logger.Debug("Config changed", "event", e.Op.String(), "file", e.Name)
		if err := pr.reloadConfig(); err != nil {
			pr.logger.Error("Reload config failed after change", "err", err)
		}
	})
	pr.logger.Info("Watching routing config for changes")
}

func (pr *PathRouter) MatchService(path string) string {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	for _, rule := range pr.rules {
		if strings.HasPrefix(path, rule.Prefix) {
			return rule.Service
		}
	}
	return pr.defaultSvc
}

func (pr *PathRouter) GetStripPrefix(path string) bool {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	for _, rule := range pr.rules {
		if strings.HasPrefix(path, rule.Prefix) {
			return rule.StripPrefix
		}
	}
	return false
}
