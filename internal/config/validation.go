package config

import (
	"fmt"
	"log/slog"
	"strings"
)

var validStrategies = map[string]bool{
	"round_robin":        true,
	"weight_round_robin": true,
	"least_conn":         true,
	"ip_hash":            true,
}

func (m *ConfigManager) validateConfig(c *Config) error {
	var errs []string

	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		errs = append(errs, fmt.Sprintf("invalid server port: %d", c.Server.Port))
	}

	if !validStrategies[c.Strategy.Strategy] {
		errs = append(errs, fmt.Sprintf("invalid strategy: %s", c.Strategy.Strategy))
	}

	if c.RedisConfig == nil {
		errs = append(errs, "cache config is missing (required)")
	}

	if len(c.BackEnds) == 0 {
		m.logger.Warn("No backend servers configured")
	} else {
		for i, be := range c.BackEnds {
			if be.Url == "" {
				errs = append(errs, fmt.Sprintf("backend #%d: missing URL", i))
			}
			if be.Weight <= 0 {
				be.Weight = 1
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("config validation failed: %s", strings.Join(errs, " | "))
	}

	return nil
}

// Kiem tra tinh hop lecua routing config
func (c *ConfigManager) validateRoutingConfig(cfg *RoutingConfig) error {
	var errs []string

	prefixSet := make(map[string]struct{})

	rules := cfg.Rules

	for i := range rules {
		idx := i + 1

		if rules[i].Prefix == "" {
			errs = append(errs, fmt.Sprintf("rule #%d: prefix is empty", idx))
			continue
		}

		//Canh bao nen co prefix bat dau la "/"
		if !strings.HasPrefix(rules[i].Prefix, "/") {
			c.logger.Warn("prefix should start with '/', normalizing it",
				slog.Int("rule", idx), slog.String("prefix", rules[i].Prefix))
			rules[i].Prefix = "/" + rules[i].Prefix
		}

		if !strings.HasPrefix(rules[i].Prefix, "/") {
			c.logger.Warn("prefix should start with '/', normalizing",
				slog.Int("rule", idx),
				slog.String("prefix", rules[i].Prefix),
			)
			rules[i].Prefix = "/" + rules[i].Prefix
		}

		//loi server_name rong, khong hop le
		if rules[i].Service == "" {
			errs = append(errs, fmt.Sprintf("rule #%d: service_name is empty (prefix: %s)", idx, rules[i].Prefix))
		}

		//kiem tra trung lap prefix
		if _, dup := prefixSet[rules[i].Prefix]; dup {
			errs = append(errs, fmt.Sprintf("duplicate prefix: '%s' at rule #%d", rules[i].Prefix, idx))
		}
		prefixSet[rules[i].Prefix] = struct{}{}

		// prefix "/" đứng trước sẽ match mọi thứ → các rule sau vô dụng
		if rules[i].Prefix == "/" && i < len(cfg.Rules)-1 {
			c.logger.Warn("prefix '/' at non-last position will shadow all following rules",
				slog.Int("rule", idx),
			)
		}
	}

	if len(cfg.Rules) == 0 {
		if cfg.DefaultService == "" {
			errs = append(errs, "no rules and no default_service → all requests will fail")
		} else {
			c.logger.Warn("no routing rules defined → all traffic goes to default_service")
		}
	} else if cfg.DefaultService == "" {
		c.logger.Warn("no default_service configured → unmatched requests will fail")
	}

	if len(errs) > 0 {
		return fmt.Errorf("routing config validation failed: %s", strings.Join(errs, "; "))
	}

	return nil
}
