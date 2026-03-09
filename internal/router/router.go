package router

import (
	"strings"

	"log/slog"

	"github.com/nhutphuongasasa/loadbalancer/internal/config"
)

type PathRouter struct {
	cfgManager *config.ConfigManager
	logger     *slog.Logger
}

// Nhan vao config path doc config, validate, apply config
func NewPathRouter(cfgManager *config.ConfigManager, logger *slog.Logger) (*PathRouter, error) {
	if logger == nil {
		logger = slog.Default()
	}

	routingCfg := cfgManager.GetRoutingConfig()

	pr := &PathRouter{
		cfgManager: cfgManager,
		logger:     logger,
	}

	pr.logger.Info("PathRouter initialized",
		slog.String("default_service", routingCfg.DefaultService),
		slog.Int("initial_rules", len(routingCfg.Rules)),
	)

	return pr, nil
}

// Match ten server name theo prefix cua path
func (pr *PathRouter) MatchService(path string) string {
	rules := pr.cfgManager.GetRoutingConfig().Rules

	for _, rule := range rules {
		if strings.HasPrefix(path, rule.Prefix) {
			return rule.Service
		}
		// else if path == "/" && len(pr.cfgManager.GetRoutingConfig().Rules) == 1 {
		// return rule.Service
		// }
	}

	return pr.cfgManager.GetRoutingConfig().DefaultService
}

// Kiem tra xem rule nao co strip_prefix=true va path match rule do hay khong
func (pr *PathRouter) StripPrefix(path string) bool {
	rules := pr.cfgManager.GetRoutingConfig().Rules

	for _, rule := range rules {
		if strings.HasPrefix(path, rule.Prefix) {
			return rule.StripPrefix
		}
	}

	return false
}
