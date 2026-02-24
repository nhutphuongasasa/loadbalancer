package registry

import (
	"context"
	"net/http"
	"time"

	"github.com/nhutphuongasasa/loadbalancer/internal/model"
)

type Registry interface {
	Register(srv *model.Server) error
	Deregister(serviceName, instanceID string) error

	UpdateStatus(srv *model.Server, alive bool)

	Discover(ctx context.Context, serviceName string) ([]*model.Server, error)

	GetUpdateChan() <-chan *model.Server
}

var GlobalBaseTransport = &http.Transport{
	MaxIdleConns:          100,
	MaxIdleConnsPerHost:   10,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
}
