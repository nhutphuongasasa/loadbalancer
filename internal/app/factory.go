package app

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/nhutphuongasasa/loadbalancer/internal/balancer/strategies"
	"github.com/nhutphuongasasa/loadbalancer/internal/cache"
	"github.com/nhutphuongasasa/loadbalancer/internal/config"
	"github.com/nhutphuongasasa/loadbalancer/internal/middleware"
	"github.com/nhutphuongasasa/loadbalancer/internal/tls"
)

func initSecuritySuite(logger *slog.Logger, cache *cache.CacheClient) *middleware.SecuritySuite {
	trafficLogger := logger.With("module", "TRAFFIC")
	securityLogger := logger.With("module", "SECURITY")

	limiter := middleware.NewIPRateLimiter(2, 5, logger)
	loggerMid := middleware.NewLogger(trafficLogger)
	sticky := middleware.NewStickyManager(securityLogger, cache)
	tracer := middleware.NewTracer(logger)

	return middleware.NewSecuritySuit(limiter, loggerMid, sticky, tracer)
}

func initTLSManager(certDir string, logger *slog.Logger) *tls.ManagerSTL {
	m, err := tls.NewManagerSTL(certDir, tls.WithLogger(logger))
	if err != nil {
		logger.Error("Failed to init TLS manager", "err", err)
		os.Exit(1)
	}
	return m
}

func initConfigManager(rootDir string) *config.ConfigManager {
	configDir := filepath.Join(rootDir, "config")
	cfgManager, err := config.NewConfigManager(configDir, func(c *config.Config) {
		slog.Info("Config reloaded")
	})
	if err != nil {
		slog.Error("Failed to init config manager", "err", err)
		os.Exit(1)
	}
	return cfgManager
}

func initStrategy(strategyName string, logger *slog.Logger) (func(string) strategies.Strategy, error) {
	switch strategyName {
	case "round_robin":
		return func(serviceName string) strategies.Strategy {
			logger.Info("Creating new RoundRobin strategy for service", "service", serviceName)
			return strategies.NewWeightedRoundRobin()
		}, nil
	case "least_conn":
		return func(serviceName string) strategies.Strategy {
			logger.Info("Creating new least connection strategy for service", "service", serviceName)
			return strategies.NewWeightedLeastConnections()
		}, nil
	case "ip_hash":
		return func(serviceName string) strategies.Strategy {
			logger.Info("Creating new ip hash strategy for service", "service", serviceName)
			return strategies.NewIPHash()
		}, nil
	default:
		return nil, fmt.Errorf("invalid strategy: %s. Supported: [round_robin, weighted_round_robin]", strategyName)
	}
}
