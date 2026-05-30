package app

import (
	"net/http"
	"strings"

	"github.com/nhutphuongasasa/loadbalancer/internal/middleware/tracer"
	"github.com/nhutphuongasasa/loadbalancer/internal/model"
)

func (a *App) GetHandler() http.Handler {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Match Service
		serviceName := a.router.MatchService(r.URL.Path)
		if serviceName == "" {
			http.Error(w, "No matching service", http.StatusNotFound)
			a.logger.Warn("No service matched", "path", r.URL.Path)
			return
		}

		//lay thong tin session tu context
		sessionPayload, exist := a.chainSecurity.Stickier().GetSessionFromContext(r)
		var backend *model.Server

		// 3. Logic chon backend
		if exist {
			// Thu lay thong tin tu cu
			if temp, ok := sessionPayload.Bindings[serviceName]; ok {
				backend = a.serverPool.GetInstanceServer(serviceName, temp.InstanceID)
			}
		}

		//neu khong co session hoac server cu da chet
		if backend == nil {
			backend = a.serverPool.PickBackend(serviceName, getClientIP(r))
		}

		if backend == nil {
			http.Error(w, "No healthy backend available", http.StatusServiceUnavailable)
			a.logger.Warn("No healthy backend", "service", serviceName)
			return
		}

		//add them backend moi vao session
		//neu tao payload moi khong thanh cong van cho request di tiep nhung khong co cookie moi
		//neu thnah cong thi bat dau flush
		newPayload, err := a.chainSecurity.Stickier().BindService(sessionPayload, serviceName, backend.InstanceID)
		if err != nil {
			a.logger.Error("Sticky bind failed", "error", err, "service", serviceName)
		} else {
			if err := a.chainSecurity.Stickier().FlushSession(w, newPayload); err != nil {
				a.logger.Warn("Sticky flush failed", "error", err)
			}
		}

		// rewrite URL
		if a.router.StripPrefix(r.URL.Path) {
			r.URL.Path = strings.TrimPrefix(r.URL.Path, "/"+serviceName)
			r.RequestURI = r.URL.RequestURI()
		}

		// 		// if a.chainSecurity.Tracer() != nil {
		// 		// a.chainSecurity.Tracer().PropagateTraceHeaders(r.Context(), r)
		// 		// }

		t, _ := tracer.TraceContextFromContext(r.Context())
		a.logger.Debug("Routing request",
			"trace_id", t.TraceID,
			"service", serviceName,
			"backend", backend.GetAddr(),
		)

		backend.ServeHTTP(w, r)
	})

	return a.chainSecurity.Wrap(handler)
}
