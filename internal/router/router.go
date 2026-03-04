package router

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

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
	mu           sync.RWMutex
	rules        []RouteRule
	defaultSvc   string
	logger       *slog.Logger
	viper        *viper.Viper
	configPath   string
	lastReload   time.Time
	reloadErrors uint64 // de export metric
	initialized  bool
}

// Nhan vao config path doc config, validate, apply config
func NewPathRouter(baseConfigDir string, logger *slog.Logger) (*PathRouter, error) {
	if logger == nil {
		logger = slog.Default()
	}

	configDir := filepath.Join(baseConfigDir, "config")
	configPath := filepath.Join(configDir, "routing.yml")

	pr := &PathRouter{
		logger:      logger,
		configPath:  configPath,
		initialized: false,
	}

	pr.viper = pr.newViper()

	// Load lan dau
	if err := pr.reloadConfig(); err != nil {
		return nil, fmt.Errorf("initial config load failed: %w", err)
	}

	// Watch config với debounce
	go pr.watchConfigWithDebounce()

	pr.logger.Info("PathRouter initialized",
		slog.String("config_path", configPath),
		slog.String("default_service", pr.defaultSvc),
		slog.Int("initial_rules", len(pr.rules)),
	)

	return pr, nil
}

// func (pr *PathRouter) newViper() *viper.Viper {
// 	v := viper.New()
// 	v.SetConfigName("routing")
// 	v.SetConfigType("yml")
// 	v.AddConfigPath(filepath.Dir(pr.configPath))

// 	v.AutomaticEnv()
// 	v.SetEnvPrefix("app")
// 	v.BindEnv("default_service", "APP_DEFAULT_SERVICE")

// 	return v
// }

func (pr *PathRouter) newViper() *viper.Viper {
	v := viper.New()
	// Chỉ định chính xác file, không dùng AddConfigPath/SetConfigName
	v.SetConfigFile(pr.configPath)

	v.AutomaticEnv()
	v.SetEnvPrefix("app")
	v.BindEnv("default_service", "APP_DEFAULT_SERVICE")

	return v
}

// Validate va ap dung rule theo config yml
func (pr *PathRouter) reloadConfig() error {
	//Doc config
	if err := pr.viper.ReadInConfig(); err != nil {
		pr.reloadErrors++
		return fmt.Errorf("failed to read config file: %w", err)
	}

	//giai ma config vao struct
	var cfg RoutingConfig
	if err := pr.viper.Unmarshal(&cfg); err != nil {
		pr.reloadErrors++
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	//kiem tra rule chi nem loi khi do la khoi dong lan dau
	if err := pr.validateRoutingConfig(&cfg); err != nil {
		if pr.initialized {
			pr.logger.Error("Invalid config, keeping old rules", "err", err)
			return nil
		}

		pr.reloadErrors++
		pr.logger.Error("Config validation failed - keeping previous config",
			slog.Any("error", err),
			slog.String("file", pr.viper.ConfigFileUsed()),
		)
		return fmt.Errorf("validation failed (old config kept): %w", err)
	}

	pr.mu.Lock()
	pr.rules = cfg.Rules
	pr.defaultSvc = cfg.DefaultService
	pr.lastReload = time.Now()
	pr.initialized = true
	pr.mu.Unlock()

	pr.logger.Info("Routing config reloaded successfully",
		slog.Int("rules_count", len(pr.rules)),
		slog.String("default_service", pr.defaultSvc),
		slog.Time("reloaded_at", pr.lastReload),
	)

	return nil
}

// Thuc hien theo doi thay doi config va hot reload dam bao du n lan thay doi trong khoang thoi gian debounce chi 1 lan reload
func (pr *PathRouter) watchConfigWithDebounce() {
	pr.viper.WatchConfig()

	//tao debunce timer dung stop de tat no ngay  de tarnh chay go rouinte
	debounceTimer := time.NewTimer(500 * time.Millisecond)
	if !debounceTimer.Stop() {
		<-debounceTimer.C
	}

	go func() {
		//dung for doi debounce time de tranh chay nhieu go routine
		for {
			<-debounceTimer.C
			pr.logger.Info("Debounce period ended - reloading config")
			if err := pr.reloadConfig(); err != nil {
				pr.logger.Error("Debounced reload failed", "error", err)
			}
		}
	}()

	pr.viper.OnConfigChange(func(e fsnotify.Event) {
		//bat cac su kien Write/Create/Rename de reload config, bo qua Delete/Chmod
		if e.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
			pr.logger.Debug("Ignoring irrelevant fsnotify event", "op", e.Op.String())
			return
		}

		pr.logger.Debug("Config change detected", "event", e.Op.String(), "file", e.Name)

		//ham stop se khien timer dung ngay lap tuc va co the luc do du lieu da vao channel
		//kiem tra xem timer day du lieu vao channel chua
		//neu co thi lay a  neu chua thi thoi
		if !debounceTimer.Stop() {
			select {
			case <-debounceTimer.C:
			default:
				<-debounceTimer.C
			}
		}
		debounceTimer.Reset(500 * time.Millisecond)
	})

	pr.logger.Info("Started watching routing config for changes with debounce")
}

// Match ten server name theo prefix cua path
func (pr *PathRouter) MatchService(path string) string {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	for _, rule := range pr.rules {
		if strings.HasPrefix(path, rule.Prefix) {
			return rule.Service
		} else if rule.Prefix == "/" && len(pr.rules) == 1 {
			return rule.Service
		}
	}
	return pr.defaultSvc
}

// Kiem tra xem rule nao co strip_prefix=true va path match rule do hay khong
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
