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

func validateConfig(c *Config) error {
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
		slog.Warn("No backend servers configured")
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
func validateRoutingConfig(cfg *RoutingConfig) error {
	var errs []string

	prefixSet := make(map[string]struct{})

	for i, rule := range cfg.Rules {
		idx := i + 1

		if rule.Prefix == "" {
			errs = append(errs, fmt.Sprintf("rule #%d: prefix is empty", idx))
			continue
		}

		//Canh bao nen co prefix bat dau la "/"
		if !strings.HasPrefix(rule.Prefix, "/") {
			slog.Warn("prefix should start with '/', normalizing it",
				slog.Int("rule", idx), slog.String("prefix", rule.Prefix))
			rule.Prefix = "/" + rule.Prefix
		}

		//loi server_name rong, khong hop le
		if rule.Service == "" {
			errs = append(errs, fmt.Sprintf("rule #%d: service_name is empty (prefix: %s)", idx, rule.Prefix))
		}

		//kiem tra trung lap prefix
		if _, dup := prefixSet[rule.Prefix]; dup {
			errs = append(errs, fmt.Sprintf("duplicate prefix: '%s' at rule #%d", rule.Prefix, idx))
		}
		prefixSet[rule.Prefix] = struct{}{}

		//kiem tra prefix la "/" match moi thu
		if rule.Prefix == "/" && len(cfg.Rules) > 1 {
			errs = append(errs, fmt.Sprintf("prefix '/' will match everything → other rules may never be used (rule #%d)", idx))
		}
	}

	if len(cfg.Rules) == 0 {
		if cfg.DefaultService == "" {
			errs = append(errs, "no rules and no default_service → all requests will fail")
		} else {
			slog.Warn("no routing rules defined → all traffic goes to default_service")
		}
	} else if cfg.DefaultService == "" {
		slog.Warn("no default_service configured → unmatched requests will fail")
	}

	if len(errs) > 0 {
		return fmt.Errorf("routing config validation failed: %s", strings.Join(errs, "; "))
	}

	return nil
}
