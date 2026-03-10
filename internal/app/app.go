package app

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"github.com/nhutphuongasasa/loadbalancer/internal/cache"
	"github.com/nhutphuongasasa/loadbalancer/internal/config"
	"github.com/nhutphuongasasa/loadbalancer/internal/middleware"
	"github.com/nhutphuongasasa/loadbalancer/internal/registry/memory"
	"github.com/nhutphuongasasa/loadbalancer/internal/registry/provider"
	"github.com/nhutphuongasasa/loadbalancer/internal/resilience"
	"github.com/nhutphuongasasa/loadbalancer/internal/router"
	"github.com/nhutphuongasasa/loadbalancer/internal/server"
	"github.com/nhutphuongasasa/loadbalancer/internal/tls"
	"github.com/nhutphuongasasa/loadbalancer/internal/utils"
)

type App struct {
	configManager  *config.ConfigManager
	serverPool     *server.ServerPool
	registry       *memory.InMemoryRegistry
	chainSecurity  *middleware.SecuritySuite
	tlsManager     *tls.ManagerSTL
	router         *router.PathRouter
	providerServer *provider.ProviderServer
	resilience     *resilience.ResilientTransport
	cacheShared    *cache.CacheClient
	logger         *slog.Logger
	ctx            context.Context
	cancel         context.CancelFunc
}

func NewApp(rootDir string) (*App, error) {
	cfgManager := initConfigManager(rootDir)
	cfg := cfgManager.GetConfig()

	logger := utils.GetLogger(cfgManager.GetSnapshot().Config.LogConfig)

	retryCfg := cfgManager.GetRetryConfig()
	cbCfg := cfgManager.GetCircuitBreakerConfig()

	providerServer := provider.NewProviderServer(retryCfg, cbCfg, logger)

	cache, err := cache.NewCacheClient(cfg.RedisConfig)

	reg := memory.NewInMemoryRegistry(logger, 10*time.Second, providerServer.GetProviderChannel())

	strategy, err := initStrategy(cfg.Strategy.Strategy, logger)
	if err != nil {
		return nil, fmt.Errorf("init strategy failed: %w", err)
	}

	rt, err := router.NewPathRouter(cfgManager, logger)
	if err != nil {
		logger.Error("Failed to init router", "err", err)
		return nil, err
	}

	pool := server.NewServerPool(
		logger,
		reg.GetUpdateChan(),
		strategy,
	)

	suite := initSecuritySuite(cfgManager, logger, cache)

	certDir := filepath.Join(rootDir, "keys")
	tlsMgr := initTLSManager(certDir, logger)

	ctx, cancel := context.WithCancel(context.Background())

	return &App{
		configManager:  cfgManager,
		serverPool:     pool,
		registry:       reg,
		router:         rt,
		chainSecurity:  suite,
		tlsManager:     tlsMgr,
		logger:         logger,
		ctx:            ctx,
		cancel:         cancel,
		cacheShared:    cache,
		providerServer: providerServer,
	}, nil
}

func (a *App) StartSubService() {
	a.logger.Info("Starting Load Balancer services...")

	if a.chainSecurity.Limiter() != nil {
		a.chainSecurity.Limiter().Start()
	}

	if a.tlsManager != nil {
		a.tlsManager.Start()
	}

	if a.registry != nil {
		a.registry.Start()
	}

}

func (a *App) StopSubService() {
	a.logger.Info("Shutting down Load Balancer...")

	a.cancel()

	if a.chainSecurity.Limiter() != nil {
		a.chainSecurity.Limiter().Stop()
	}

	if a.tlsManager != nil {
		a.tlsManager.Stop()
	}

	if a.registry != nil {
		a.registry.Stop()
	}

	a.serverPool.Close()

	a.logger.Info("Shutdown completed")
}

func (a *App) GetTLSManager() *tls.ManagerSTL {
	return a.tlsManager
}

func (a *App) GetConfigManager() *config.ConfigManager {
	return a.configManager
}

func getClientIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr // fallback nếu lỗi
	}
	return ip
}

func (a *App) GetProviderServer() *provider.ProviderServer {
	return a.providerServer
}
