package config

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

var (
	once     sync.Once
	instance *slog.Logger
)

type ConfigManager struct {
	Config       *Config
	RouterCfg    *RoutingConfig
	RetryCfg     *RetryConfig
	reloadErrors int
	viper        *viper.Viper
	onChange     func(*Config)
	mux          sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	startOne     sync.Once
	stopOne      sync.Once
	wg           sync.WaitGroup
	lastReload   time.Time
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

func (c *ConfigManager) GetHealthCheckInterval() time.Duration {
	duration, err := time.ParseDuration(c.Config.Server.HealthCheckInterval)
	if err != nil {
		c.logger.Error("Failed to parse health check interval", "error", err)
		return 10 * time.Second
	}
	return duration
}

func (c *ConfigManager) watchConfigWithDebounce() {
	defer c.wg.Done()

	c.viper.WatchConfig()

	//tao debunce timer dung stop de tat no ngay  de tarnh chay go rouinte
	debounceTimer := time.NewTimer(500 * time.Millisecond)
	if !debounceTimer.Stop() {
		<-debounceTimer.C
	}

	go func() {
		//dung for doi debounce time de tranh chay nhieu go routine
		for {
			<-debounceTimer.C
			c.logger.Info("Debounce period ended - reloading config")
			if err := c.reloadConfig(); err != nil {
				c.logger.Error("Debounced reload failed", "error", err)
			}
		}
	}()

	c.viper.OnConfigChange(func(e fsnotify.Event) {
		//bat cac su kien Write/Create/Rename de reload config, bo qua Delete/Chmod
		if e.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
			c.logger.Debug("Ignoring irrelevant fsnotify event", "op", e.Op.String())
			return
		}

		c.logger.Debug("Config change detected", "event", e.Op.String(), "file", e.Name)

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

	c.logger.Info("Watcher goroutine received signal and is exiting...")
}

func (c *ConfigManager) loadConfig(configDir string) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yml")
	v.AddConfigPath(configDir)

	v.AutomaticEnv()                            //bat che do quet bien moi turong tu dong
	v.SetEnvPrefix("app")                       //chi nhan cac bien moi turong co prefix la APP_
	v.BindEnv("server.port", "APP_SERVER_PORT") //neu moi turong co bien APP_SERVER_PORT thi ghi de no len server.port

	c.viper = v

	c.reloadConfig()
}

func (c *ConfigManager) reloadConfig() error {
	if err := c.viper.ReadInConfig(); err != nil {
		c.reloadErrors++
		c.logger.Error("Failed to read config file, using defaults", "err", err)
		return err
	}

	cfg, routerCfg, retryCfg, err := unMarshalConfig(c.viper)
	if err != nil {
		return err
	}

	if err = validateConfig(cfg); err != nil {
		if c.firstInitial {
			c.reloadErrors++
			c.logger.Error("Initial config validation failed")
			os.Exit(1)
		} else {
			c.logger.Warn("Invalid config, keeping old base config", "err", err)
			return err
		}
	}

	if err = validateRoutingConfig(routerCfg); err != nil {
		if c.firstInitial {
			c.reloadErrors++
			c.logger.Error("Initial router config validation failed")
			os.Exit(1)

		} else {
			c.logger.Error("Invalid config, keeping old rules", "err", err)
			return err
		}
	}

	if retryCfg == nil {
		c.reloadErrors++
		c.logger.Warn("Initial retry config validation failed")
		if c.firstInitial {
			// retryCfg =
		}
	}

	c.mux.Lock()
	c.Config = cfg
	c.RouterCfg = routerCfg
	c.RetryCfg = retryCfg
	c.lastReload = time.Now()
	c.mux.Unlock()

	c.logger.Info("Config reloaded successfully")

	c.firstInitial = false

	return nil
}

func (c *ConfigManager) GetConfig() *Config {
	c.mux.RLock()
	defer c.mux.RUnlock()
	return c.Config
}

func (c *ConfigManager) GetRoutingConfig() *RoutingConfig {
	c.mux.RLock()
	defer c.mux.RUnlock()
	return c.RouterCfg
}

func (c *ConfigManager) GetRetryConfig() *RetryConfig {
	c.mux.RLock()
	defer c.mux.RUnlock()
	return c.RetryCfg
}

func (m *ConfigManager) SetOnChange(fn func(*Config)) {
	m.mux.Lock()
	m.onChange = fn
	m.mux.Unlock()
}

func getDefaultLogger() *slog.Logger {
	once.Do(func() {
		level := slog.LevelDebug

		opts := &slog.HandlerOptions{
			Level:     level,
			AddSource: level == slog.LevelDebug,
		}

		logFile, err := os.OpenFile("app.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
		var output io.Writer = os.Stdout // Mặc định là Terminal

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
