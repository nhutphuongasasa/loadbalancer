package config

import (
	"log/slog"
	"os"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

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

	c.logger.Info("Watcher goroutine is now active")

	<-c.ctx.Done()

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

	err := c.reloadConfig()
	if err != nil {
		os.Exit(1)
	}
}

func (c *ConfigManager) reloadConfig() error {
	if err := c.viper.ReadInConfig(); err != nil {
		c.reloadErrors++
		c.logger.Error("Failed to read config file, using defaults", "err", err)
		return err
	}

	resultCfg, err := unMarshalConfig(c.viper)
	if err != nil {
		return err
	}

	if err = c.validateConfig(resultCfg.config); err != nil {
		if c.firstInitial {
			c.reloadErrors++
			c.logger.Error("Initial config validation failed")
			os.Exit(1)
		} else {
			c.logger.Warn("Invalid config, keeping old base config", "err", err)
			return err
		}
	}

	if err = c.validateRoutingConfig(resultCfg.router); err != nil {
		if c.firstInitial {
			c.reloadErrors++
			c.logger.Error("Initial router config validation failed")
			os.Exit(1)

		} else {
			c.logger.Error("Invalid config, keeping old rules", "err", err)
			return err
		}
	}

	// RetryConfig optional — dùng default nếu không có trong file
	if resultCfg.retry == nil || resultCfg.retry.MaxRetries == 0 {
		c.logger.Warn("Retry config missing or incomplete, using defaults")
		resultCfg.retry = &RetryConfig{
			MaxRetries:   3,
			BaseDelay:    200 * time.Millisecond,
			MaxDelay:     5 * time.Second,
			JitterFactor: 0.1,
		}
	}

	// RateLimitConfig optional — dùng default nếu không có
	if resultCfg.ratelimit == nil || resultCfg.ratelimit.RequestsPerSecond == 0 {
		c.logger.Warn("Rate limit config missing or incomplete, using defaults")
		resultCfg.ratelimit = DefaultRateLimitConfig()
	}

	if resultCfg.circuitBreaker == nil {
		c.logger.Warn("CircuitBreaker config missing or incomplete, using defaults")
		resultCfg.circuitBreaker = DefaultCircuitBreakerConfig()
	}

	newSnapshot := &ConfigSnapshot{
		Config:         resultCfg.config,
		Routing:        resultCfg.router,
		Retry:          resultCfg.retry,
		RateLimit:      resultCfg.ratelimit,
		CircuitBreaker: resultCfg.circuitBreaker,
		LastReload:     time.Now(),
	}

	c.snapshot.Store(newSnapshot)

	c.logger.Info("Config reloaded successfully",
		slog.Time("reloaded_at", newSnapshot.LastReload),
		slog.Bool("rate_limit_enabled", resultCfg.ratelimit.Enabled),
		slog.Float64("rps", resultCfg.ratelimit.RequestsPerSecond),
	)

	c.firstInitial = false
	return nil
}
