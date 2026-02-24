package config

import "log/slog"

var validStrategies = map[string]bool{
	"round_robin":        true,
	"weight_round_robin": true,
	"least_conn":         true,
	"ip_hash":            true,
}

func validateConfig(c *Config) bool {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		slog.Error("Invalid server port", "port", c.Server.Port)
		return false
	}

	if !validStrategies[c.Strategy.Strategy] {
		slog.Error("Invalid load balancing strategy", "strategy", c.Strategy.Strategy)
		return false
	}

	if c.RedisConfig == nil {
		slog.Error("Can not be start load balancer without cache")
	}

	if len(c.BackEnds) == 0 {
		slog.Warn("No backend servers configured")
	} else {
		for i, be := range c.BackEnds {
			if be.Url == "" {
				slog.Error("Backend missing URL", "index", i)
				return false
			}
			if be.Weight <= 0 {
				be.Weight = 1
			}
		}
	}

	return true
}
