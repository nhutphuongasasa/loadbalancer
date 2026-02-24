package router

import (
	"fmt"
	"strings"
)

func (pr *PathRouter) validateRoutingConfig(cfg *RoutingConfig) (bool, error) {
	var errs []string

	if len(cfg.Rules) == 0 {
		if cfg.DefaultService == "" {
			errs = append(errs, "no routing rules and no default_service configured")
		}
	} else {
		if cfg.DefaultService == "" {
			pr.logger.Warn("no default_service configured, requests without matching prefix will fail")
		}
	}

	prefixSet := make(map[string]struct{})
	for i, rule := range cfg.Rules {
		ruleIdx := i + 1 // cho log dễ đọc

		if rule.Prefix == "" {
			errs = append(errs, fmt.Sprintf("rule #%d: prefix is empty", ruleIdx))
			continue
		}

		if !strings.HasPrefix(rule.Prefix, "/") {
			pr.logger.Warn("rule prefix should start with '/', got", "ruleIdx", ruleIdx, "rule.Prefix", rule.Prefix)
		}

		if rule.Service == "" {
			errs = append(errs, fmt.Sprintf("rule #%d: service_name is empty (prefix: %s)", ruleIdx, rule.Prefix))
			continue
		}

		if _, exists := prefixSet[rule.Prefix]; exists {
			errs = append(errs, fmt.Sprintf("duplicate prefix detected: '%s' at index %d", rule.Prefix, ruleIdx))
		}
		prefixSet[rule.Prefix] = struct{}{}

		if rule.Prefix == "/" && len(cfg.Rules) > 1 {
			pr.logger.Warn("rule #%d: prefix '/' will match everything, other rules may be ignored", "ruleInx", ruleIdx)
		}
	}

	if len(errs) > 0 {
		return false, fmt.Errorf("routing config validation failed: %s", strings.Join(errs, "; "))
	}

	if len(cfg.Rules) == 0 {
		pr.logger.Warn("no routing rules defined, all traffic will go to default_service")
	}

	return true, nil
}
