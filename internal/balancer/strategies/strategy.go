package strategies

import (
	"github.com/nhutphuongasasa/loadbalancer/internal/model"
)

type Strategy interface {
	Pick(backends []*model.Server, clientIP string) *model.Server
}
