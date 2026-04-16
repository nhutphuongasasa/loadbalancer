package registry

import (
	"context"
	"net/http"
	"time"

	"github.com/nhutphuongasasa/loadbalancer/internal/model"
)

type RegistryAdapter interface {
	Register(srv *model.Server) error
	Deregister(serviceName, instanceID string) error

	UpdateStatus(serviceName, instanceID string, alive bool)

	Discover(ctx context.Context, serviceName string) ([]*model.Server, error)

	GetUpdateChan() <-chan *model.Server

	ListAll() map[string][]*model.Server

	GetVersionData() VersionData

	RefreshAllChecksums()
	GetChecksum() map[string]uint64
	CompareAndFetch(remoteChecksums map[string]uint64) map[string]map[string]*model.Server
}

var GlobalBaseTransport = &http.Transport{
	MaxIdleConns:          100,
	MaxIdleConnsPerHost:   10,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
}
