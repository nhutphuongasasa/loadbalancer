package router

import (
	"log/slog"
	"testing"

	"github.com/nhutphuongasasa/loadbalancer/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================
// Helper — tạo ConfigManager với snapshot được set sẵn
// ============================================================

func newStubConfigManager(rules []config.RouteRule, defaultSvc string) *config.ConfigManager {
	cm := &config.ConfigManager{}
	// FIX: dùng StoreSnapshot thay vì gọi GetSnapshot() rồi assign
	// GetSnapshot() trả về nil khi chưa có snapshot → panic
	cm.StoreSnapshot(&config.ConfigSnapshot{
		Routing: &config.RoutingConfig{
			Rules:          rules,
			DefaultService: defaultSvc,
		},
	})
	return cm
}

func routerWithRules(rules []config.RouteRule, defaultSvc string) *PathRouter {
	return &PathRouter{
		cfgManager: newStubConfigManager(rules, defaultSvc),
		logger:     slog.Default(),
	}
}

// ============================================================
// NewPathRouter
// ============================================================

func TestNewPathRouter_NilLogger_UsesDefault(t *testing.T) {
	cm := newStubConfigManager(nil, "default")
	pr, err := NewPathRouter(cm, nil)
	require.NoError(t, err)
	require.NotNil(t, pr)
	assert.NotNil(t, pr.logger)
}

func TestNewPathRouter_Success(t *testing.T) {
	rules := []config.RouteRule{
		{Prefix: "/api", Service: "api-svc"},
	}
	cm := newStubConfigManager(rules, "fallback")
	pr, err := NewPathRouter(cm, slog.Default())
	require.NoError(t, err)
	require.NotNil(t, pr)
}

// ============================================================
// MatchService
// ============================================================

func TestMatchService_FirstRuleWins(t *testing.T) {
	pr := routerWithRules([]config.RouteRule{
		{Prefix: "/api/v1", Service: "svc-v1"},
		{Prefix: "/api", Service: "svc-api"},
	}, "default")
	assert.Equal(t, "svc-v1", pr.MatchService("/api/v1/users"))
}

func TestMatchService_SecondRule(t *testing.T) {
	pr := routerWithRules([]config.RouteRule{
		{Prefix: "/api/v1", Service: "svc-v1"},
		{Prefix: "/api", Service: "svc-api"},
	}, "default")
	assert.Equal(t, "svc-api", pr.MatchService("/api/v2/orders"))
}

func TestMatchService_NoMatch_ReturnsDefault(t *testing.T) {
	pr := routerWithRules([]config.RouteRule{
		{Prefix: "/admin", Service: "admin-svc"},
	}, "default-svc")
	assert.Equal(t, "default-svc", pr.MatchService("/home"))
}

func TestMatchService_EmptyRules_ReturnsDefault(t *testing.T) {
	pr := routerWithRules(nil, "only-default")
	assert.Equal(t, "only-default", pr.MatchService("/anything"))
}

func TestMatchService_RootPrefix_MatchesAll(t *testing.T) {
	pr := routerWithRules([]config.RouteRule{
		{Prefix: "/", Service: "root-svc"},
	}, "fallback")
	assert.Equal(t, "root-svc", pr.MatchService("/anything"))
	assert.Equal(t, "root-svc", pr.MatchService("/api/v1/users"))
}

func TestMatchService_EmptyPath_NoMatch(t *testing.T) {
	pr := routerWithRules([]config.RouteRule{
		{Prefix: "/", Service: "root"},
	}, "fallback")
	// "" không HasPrefix "/" → fallback
	assert.Equal(t, "fallback", pr.MatchService(""))
}

func TestMatchService_ExactPrefixMatch(t *testing.T) {
	pr := routerWithRules([]config.RouteRule{
		{Prefix: "/api", Service: "api-svc"},
	}, "default")
	assert.Equal(t, "api-svc", pr.MatchService("/api"))
	assert.Equal(t, "api-svc", pr.MatchService("/api/"))
	assert.Equal(t, "api-svc", pr.MatchService("/api/users"))
}

// ============================================================
// StripPrefix
// ============================================================

func TestStripPrefix_True(t *testing.T) {
	pr := routerWithRules([]config.RouteRule{
		{Prefix: "/api/v1", Service: "svc", StripPrefix: true},
	}, "")
	assert.True(t, pr.StripPrefix("/api/v1/users"))
}

func TestStripPrefix_False(t *testing.T) {
	pr := routerWithRules([]config.RouteRule{
		{Prefix: "/api/v2", Service: "svc", StripPrefix: false},
	}, "")
	assert.False(t, pr.StripPrefix("/api/v2/data"))
}

func TestStripPrefix_NoMatch_ReturnsFalse(t *testing.T) {
	pr := routerWithRules([]config.RouteRule{
		{Prefix: "/admin", StripPrefix: true},
	}, "")
	assert.False(t, pr.StripPrefix("/public/page"))
}

// ============================================================
// validateRoutingConfig
// ============================================================

// func TestValidateRoutingConfig_Valid(t *testing.T) {
// 	pr := &PathRouter{logger: slog.Default()}
// 	cfg := &config.RoutingConfig{
// 		Rules: []config.RouteRule{
// 			{Prefix: "/api", Service: "api-svc"},
// 			{Prefix: "/admin", Service: "admin-svc"},
// 		},
// 		DefaultService: "fallback",
// 	}
// 	assert.NoError(t, pr.validateRoutingConfig(cfg))
// }

// func TestValidateRoutingConfig_EmptyPrefix(t *testing.T) {
// 	pr := &PathRouter{logger: slog.Default()}
// 	cfg := &config.RoutingConfig{
// 		Rules: []config.RouteRule{{Prefix: "", Service: "svc"}},
// 	}
// 	err := pr.validateRoutingConfig(cfg)
// 	require.Error(t, err)
// 	assert.Contains(t, err.Error(), "prefix is empty")
// }

// func TestValidateRoutingConfig_EmptyService(t *testing.T) {
// 	pr := &PathRouter{logger: slog.Default()}
// 	cfg := &config.RoutingConfig{
// 		Rules: []config.RouteRule{{Prefix: "/api", Service: ""}},
// 	}
// 	err := pr.validateRoutingConfig(cfg)
// 	require.Error(t, err)
// 	assert.Contains(t, err.Error(), "service_name is empty")
// }

// func TestValidateRoutingConfig_DuplicatePrefix(t *testing.T) {
// 	pr := &PathRouter{logger: slog.Default()}
// 	cfg := &config.RoutingConfig{
// 		Rules: []config.RouteRule{
// 			{Prefix: "/api", Service: "svc1"},
// 			{Prefix: "/api", Service: "svc2"},
// 		},
// 	}
// 	err := pr.validateRoutingConfig(cfg)
// 	require.Error(t, err)
// 	assert.Contains(t, err.Error(), "duplicate prefix")
// }

// func TestValidateRoutingConfig_NormalizePrefix(t *testing.T) {
// 	pr := &PathRouter{logger: slog.Default()}
// 	cfg := &config.RoutingConfig{
// 		Rules:          []config.RouteRule{{Prefix: "api", Service: "api-svc"}},
// 		DefaultService: "fallback",
// 	}
// 	err := pr.validateRoutingConfig(cfg)
// 	assert.NoError(t, err)
// 	// validate.go cần dùng pointer (for i := range) để normalize lưu lại được
// 	assert.Equal(t, "/api", cfg.Rules[0].Prefix)
// }

// func TestValidateRoutingConfig_NoRulesWithDefault_NoError(t *testing.T) {
// 	pr := &PathRouter{logger: slog.Default()}
// 	cfg := &config.RoutingConfig{DefaultService: "fallback"}
// 	assert.NoError(t, pr.validateRoutingConfig(cfg))
// }

// func TestValidateRoutingConfig_NoRulesNoDefault_Error(t *testing.T) {
// 	pr := &PathRouter{logger: slog.Default()}
// 	err := pr.validateRoutingConfig(&config.RoutingConfig{})
// 	require.Error(t, err)
// 	assert.Contains(t, err.Error(), "all requests will fail")
// }

// func TestValidateRoutingConfig_MultipleErrors(t *testing.T) {
// 	pr := &PathRouter{logger: slog.Default()}
// 	cfg := &config.RoutingConfig{
// 		Rules: []config.RouteRule{
// 			{Prefix: "", Service: ""}, // empty prefix
// 			{Prefix: "/api", Service: ""},
// 		},
// 	}
// 	assert.Error(t, pr.validateRoutingConfig(cfg))
// }
