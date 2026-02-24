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

// func defaultConfig() *Config {
// 	return &Config{
// 		Server: &ServerConfig{
// 			Port:                8080,
// 			HealthCheckInterval: "10s",
// 		},
// 		Strategy: &StrategyConfig{
// 			Strategy: "round_robin",
// 		},
// 		BackEnds: []*BackEndConfig{},
// 		LogConfig: &LogConfig{
// 			Level:  "debug",
// 			Format: "json",
// 		},
// 	}
// }

func unMarshalConfig(v *viper.Viper) (*Config, error) {
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
