package app

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/nhutphuongasasa/loadbalancer/internal/cache"
	"github.com/nhutphuongasasa/loadbalancer/internal/config"
	"github.com/nhutphuongasasa/loadbalancer/internal/middleware"
	"github.com/nhutphuongasasa/loadbalancer/internal/model"
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

	logger := utils.GetLogger(cfgManager)

	providerServer := provider.NewProviderServer(logger)

	cache, err := cache.NewCacheClient(cfgManager.GetConfig().RedisConfig)

	reg := memory.NewInMemoryRegistry(logger, 10*time.Second, providerServer.GetProviderChannel())

	cfg := cfgManager.GetConfig()
	strategy, err := initStrategy(cfg.Strategy.Strategy, logger)
	if err != nil {
		return nil, fmt.Errorf("init strategy failed: %w", err)
	}

	rt, err := router.NewPathRouter(rootDir, logger)
	if err != nil {
		logger.Error("Failed to init router", "err", err)
		return nil, err
	}

	pool := server.NewServerPool(
		logger,
		reg.GetUpdateChan(),
		strategy,
	)

	suite := initSecuritySuite(logger, cache)

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

func (a *App) GetHandler() http.Handler {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		//ap dung rule  duoc cung cap de lay server name
		serviceName := a.router.MatchService(r.URL.Path)
		if serviceName == "" {
			http.Error(w, "No matching service", http.StatusNotFound)
			a.logger.Warn("No service matched", "path", r.URL.Path)
			return
		}

		//lay thong tin cac server name instanceId tu cache nho sessionId
		var backend *model.Server
		serverPair, ok := a.chainSecurity.Stickier().GetBackendFromContext(r)

		//khong co thong tin thi set cookie moi
		if !ok {
			backend = a.serverPool.PickBackend(serviceName, getClientIP(r))
			a.chainSecurity.Stickier().SetStickySession(w, serviceName, backend.InstanceID)
		} else {
			//cache co chua thong tin ve server name va instanceId
			for _, v := range serverPair {
				if v.ServerName == serviceName {
					backend = a.serverPool.GetInstanceServer(serviceName, v.InstanceId)
					break
				}
			}

			if backend == nil {
				//cache khong co thong tin ghi them vao cache
				cacheKey := a.chainSecurity.Stickier().GetCacheKeyFromContext(r)
				backend = a.serverPool.PickBackend(serviceName, getClientIP(r))

				a.cacheShared.SetArray(a.ctx, cacheKey, append(serverPair, &model.ServerPair{
					ServerName: serviceName,
					InstanceId: backend.InstanceID,
				}), 0)
			}
		}

		if backend == nil {
			http.Error(w, "No healthy backend available", http.StatusServiceUnavailable)
			a.logger.Warn("No healthy backend", "service", serviceName)
			return
		}

		if a.router.GetStripPrefix(r.URL.Path) {
			r.URL.Path = strings.TrimPrefix(r.URL.Path, "/"+serviceName)
			r.RequestURI = r.URL.RequestURI()
		}

		// if a.chainSecurity.Tracer() != nil {
		// a.chainSecurity.Tracer().PropagateTraceHeaders(r.Context(), r)
		// }

		a.logger.Debug("Routed request",
			"trace_id", middleware.TraceContextFromContext(r.Context()).TraceID,
			"backend", backend.GetAddr(),
		)

		backend.ServeHTTP(w, r)
		a.logger.Debug("Routed request", "path", r.URL.Path, "service", serviceName, "backend", backend.GetAddr())
	})

	return a.chainSecurity.Wrap(handler)
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
