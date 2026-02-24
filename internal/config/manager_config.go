package config

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

type ConfigManager struct {
	mux      sync.RWMutex
	config   *Config
	viper    *viper.Viper // ban nang cap cua fsnoify wacther
	onChange func(*Config)
	ctx      context.Context
	cancel   context.CancelFunc
	startOne sync.Once
	stopOne  sync.Once
	wg       sync.WaitGroup
}

func NewConfigManager(configDir string, onChange func(*Config)) (*ConfigManager, error) {

	m := &ConfigManager{
		onChange: onChange,
	}

	m.loadConfig(configDir)

	slog.Info("Config manager initialized with hot-reload")

	return m, nil
}

func (m *ConfigManager) Start() error {
	var err error
	m.startOne.Do(func() {
		m.ctx, m.cancel = context.WithCancel(context.Background())
		m.wg.Add(1)

		go m.watchConfig()
		slog.Info("Starting Watcher config file")
	})

	return err
}

func (m *ConfigManager) Stop() {
	m.stopOne.Do(func() {
		if m.cancel != nil {
			slog.Info("Stopping watcher config manager")
			m.cancel()
		}
		if m.viper != nil {
			m.wg.Wait()
		}
		slog.Info("Complete stop watcher config manger")
	})
}

func (c *ConfigManager) GetHealthCheckInterval() time.Duration {
	duration, err := time.ParseDuration(c.config.Server.HealthCheckInterval)
	if err != nil {
		slog.Error("Failed to parse health check interval", "error", err)
		return 10 * time.Second
	}
	return duration
}

func (c *ConfigManager) watchConfig() {
	defer c.wg.Done()

	c.viper.WatchConfig()

	c.viper.OnConfigChange(func(e fsnotify.Event) {
		select {
		case <-c.ctx.Done():
			return
		default:
			slog.Debug("Config file changed", "event", e.Op.String())
			c.reloadConfig()
			if c.onChange != nil {
				c.onChange(c.config)
			}
		}
	})

	slog.Info("Watcher goroutine is now active and waiting for changes")

	<-c.ctx.Done()

	slog.Info("Watcher goroutine received signal and is exiting...")
}

func (c *ConfigManager) loadConfig(configDir string) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yml")
	v.AddConfigPath(configDir)

	v.AutomaticEnv()                            //bat che do quet bien moi turong tu dong
	v.SetEnvPrefix("app")                       //chi nhan cac bien moi turong co prefix la APP_
	v.BindEnv("server.port", "APP_SERVER_PORT") //neu moi turong co bien SERVER_PORT thi ghi de no len server.port

	c.viper = v

	c.reloadConfig()
}

func (c *ConfigManager) reloadConfig() {
	if err := c.viper.ReadInConfig(); err != nil {
		slog.Error("Failed to read config file, using defaults", "err", err)
		return
	}

	cfg, err := unMarshalConfig(c.viper)
	if err != nil {
		err = fmt.Errorf("initial config unmarshal failed: %w", err)
	}

	if !validateConfig(cfg) {
		slog.Error("Initial config validation failed")
	}

	c.mux.Lock()
	c.config = cfg
	c.mux.Unlock()

	slog.Info("Config reloaded successfully")
}

func (c *ConfigManager) GetConfig() *Config {
	return c.config
}

func (c *ConfigManager) GetPortServer() int {
	return c.config.Server.Port
}
