package strategies

import "github.com/nhutphuongasasa/loadbalancer/internal/model"

type Strategy interface {
	Update(backends []*model.Server)
	Pick(clientIP string) *model.Server
}
