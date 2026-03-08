package app

import (
	"net/http"
	"strings"

	"github.com/nhutphuongasasa/loadbalancer/internal/middleware/tracer"
	"github.com/nhutphuongasasa/loadbalancer/internal/model"
)

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

		tracer, _ := tracer.TraceContextFromContext(r.Context())
		a.logger.Debug("Routed request",
			"trace_id", tracer.TraceID,
			"backend", backend.GetAddr(),
		)

		backend.ServeHTTP(w, r)
		a.logger.Debug("Routed request", "path", r.URL.Path, "service", serviceName, "backend", backend.GetAddr())
	})

	return handler

	// return a.chainSecurity.Wrap(handler)
}
