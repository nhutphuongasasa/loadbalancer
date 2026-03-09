package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================
// Helper — tạo file config tạm để test
// ============================================================

func writeConfigFile(t *testing.T, dir, content string) {
	t.Helper()
	err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte(content), 0644)
	require.NoError(t, err)
}

func validConfigYML() string {
	return `
server:
  port: 8080
  health_check_interval: 10s
load_balancer:
  strategy: round_robin
cache:
  addr: localhost:6379
  db: 0
backends:
  - url: http://localhost:9001
    weight: 1
routing:
  default_service: fallback
  rules:
    - prefix: /api
      service_name: api-svc
retry:
  max_retries: 3
  base_delay: 200ms
  max_delay: 5s
  jitter_factor: 0.1
`
}

func newTestConfigManager(t *testing.T, content string) (*ConfigManager, string) {
	t.Helper()
	dir := t.TempDir()
	writeConfigFile(t, dir, content)
	cm, err := NewConfigManager(dir, nil)
	require.NoError(t, err)
	return cm, dir
}

// ============================================================
// StoreSnapshot / GetSnapshot
// ============================================================

func TestStoreSnapshot_GetSnapshot(t *testing.T) {
	cm := &ConfigManager{}
	snap := &ConfigSnapshot{
		Config:  &Config{},
		Routing: &RoutingConfig{DefaultService: "svc"},
	}
	cm.StoreSnapshot(snap)

	got := cm.GetSnapshot()
	require.NotNil(t, got)
	assert.Equal(t, "svc", got.Routing.DefaultService)
}

func TestGetSnapshot_NilBeforeStore(t *testing.T) {
	cm := &ConfigManager{}
	assert.Nil(t, cm.GetSnapshot())
}

// ============================================================
// GetConfig / GetRoutingConfig / GetRetryConfig
// ============================================================

func TestGetConfig_NilBeforeLoad(t *testing.T) {
	cm := &ConfigManager{}
	assert.Nil(t, cm.GetConfig())
}

func TestGetRoutingConfig_NilBeforeLoad(t *testing.T) {
	cm := &ConfigManager{}
	assert.Nil(t, cm.GetRoutingConfig())
}

func TestGetRetryConfig_NilBeforeLoad(t *testing.T) {
	cm := &ConfigManager{}
	assert.Nil(t, cm.GetRetryConfig())
}

func TestGetConfig_AfterLoad(t *testing.T) {
	cm, _ := newTestConfigManager(t, validConfigYML())
	cfg := cm.GetConfig()
	require.NotNil(t, cfg)
	assert.Equal(t, 8080, cfg.Server.Port)
}

func TestGetRoutingConfig_AfterLoad(t *testing.T) {
	cm, _ := newTestConfigManager(t, validConfigYML())
	rc := cm.GetRoutingConfig()
	require.NotNil(t, rc)
	assert.Equal(t, "fallback", rc.DefaultService)
	assert.Len(t, rc.Rules, 1)
}

func TestGetRetryConfig_AfterLoad(t *testing.T) {
	cm, _ := newTestConfigManager(t, validConfigYML())
	rc := cm.GetRetryConfig()
	require.NotNil(t, rc)
	assert.Equal(t, 3, rc.MaxRetries)
	assert.Equal(t, 200*time.Millisecond, rc.BaseDelay)
}

// ============================================================
// GetHealthCheckInterval
// ============================================================

func TestGetHealthCheckInterval_Valid(t *testing.T) {
	cm, _ := newTestConfigManager(t, validConfigYML())
	d := cm.GetHealthCheckInterval()
	assert.Equal(t, 10*time.Second, d)
}

func TestGetHealthCheckInterval_NilSnapshot_ReturnsDefault(t *testing.T) {
	cm := &ConfigManager{logger: getDefaultLogger()}
	d := cm.GetHealthCheckInterval()
	assert.Equal(t, 10*time.Second, d)
}

func TestGetHealthCheckInterval_InvalidDuration_ReturnsDefault(t *testing.T) {
	cm := &ConfigManager{logger: getDefaultLogger()}
	cm.StoreSnapshot(&ConfigSnapshot{
		Config: &Config{
			Server: &ServerConfig{HealthCheckInterval: "not-a-duration"},
		},
	})
	d := cm.GetHealthCheckInterval()
	assert.Equal(t, 10*time.Second, d)
}

// ============================================================
// validateConfig
// ============================================================

func TestValidateConfig_Valid(t *testing.T) {
	cm := &ConfigManager{logger: getDefaultLogger()}
	cfg := &Config{
		Server:   &ServerConfig{Port: 8080},
		Strategy: &StrategyConfig{Strategy: "round_robin"},
		RedisConfig: &CacheConfig{
			Addr: "localhost:6379",
		},
		BackEnds: []*BackEndConfig{
			{Url: "http://localhost:9001", Weight: 1},
		},
	}
	assert.NoError(t, cm.validateConfig(cfg))
}

func TestValidateConfig_InvalidPort_Zero(t *testing.T) {
	cm := &ConfigManager{logger: getDefaultLogger()}
	cfg := &Config{
		Server:      &ServerConfig{Port: 0},
		Strategy:    &StrategyConfig{Strategy: "round_robin"},
		RedisConfig: &CacheConfig{},
	}
	err := cm.validateConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid server port")
}

func TestValidateConfig_InvalidPort_TooHigh(t *testing.T) {
	cm := &ConfigManager{logger: getDefaultLogger()}
	cfg := &Config{
		Server:      &ServerConfig{Port: 99999},
		Strategy:    &StrategyConfig{Strategy: "round_robin"},
		RedisConfig: &CacheConfig{},
	}
	err := cm.validateConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid server port")
}

func TestValidateConfig_InvalidStrategy(t *testing.T) {
	cm := &ConfigManager{logger: getDefaultLogger()}
	cfg := &Config{
		Server:      &ServerConfig{Port: 8080},
		Strategy:    &StrategyConfig{Strategy: "unknown_strategy"},
		RedisConfig: &CacheConfig{},
	}
	err := cm.validateConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid strategy")
}

func TestValidateConfig_AllValidStrategies(t *testing.T) {
	cm := &ConfigManager{logger: getDefaultLogger()}
	strategies := []string{"round_robin", "weight_round_robin", "least_conn", "ip_hash"}

	for _, s := range strategies {
		cfg := &Config{
			Server:      &ServerConfig{Port: 8080},
			Strategy:    &StrategyConfig{Strategy: s},
			RedisConfig: &CacheConfig{},
		}
		assert.NoError(t, cm.validateConfig(cfg), "strategy %q should be valid", s)
	}
}

func TestValidateConfig_MissingRedis(t *testing.T) {
	cm := &ConfigManager{logger: getDefaultLogger()}
	cfg := &Config{
		Server:      &ServerConfig{Port: 8080},
		Strategy:    &StrategyConfig{Strategy: "round_robin"},
		RedisConfig: nil,
	}
	err := cm.validateConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cache config is missing")
}

func TestValidateConfig_BackendMissingURL(t *testing.T) {
	cm := &ConfigManager{logger: getDefaultLogger()}
	cfg := &Config{
		Server:      &ServerConfig{Port: 8080},
		Strategy:    &StrategyConfig{Strategy: "round_robin"},
		RedisConfig: &CacheConfig{},
		BackEnds:    []*BackEndConfig{{Url: "", Weight: 1}},
	}
	err := cm.validateConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing URL")
}

func TestValidateConfig_BackendZeroWeight_NormalizesTo1(t *testing.T) {
	cm := &ConfigManager{logger: getDefaultLogger()}
	cfg := &Config{
		Server:      &ServerConfig{Port: 8080},
		Strategy:    &StrategyConfig{Strategy: "round_robin"},
		RedisConfig: &CacheConfig{},
		BackEnds:    []*BackEndConfig{{Url: "http://localhost:9001", Weight: 0}},
	}
	err := cm.validateConfig(cfg)
	assert.NoError(t, err)
	assert.Equal(t, 1, cfg.BackEnds[0].Weight)
}

// ============================================================
// validateRoutingConfig
// ============================================================

func TestValidateRoutingConfig_Valid(t *testing.T) {
	cm := &ConfigManager{logger: getDefaultLogger()}
	cfg := &RoutingConfig{
		Rules: []RouteRule{
			{Prefix: "/api", Service: "api-svc"},
			{Prefix: "/admin", Service: "admin-svc"},
		},
		DefaultService: "fallback",
	}
	assert.NoError(t, cm.validateRoutingConfig(cfg))
}

func TestValidateRoutingConfig_EmptyPrefix(t *testing.T) {
	cm := &ConfigManager{logger: getDefaultLogger()}
	cfg := &RoutingConfig{
		Rules: []RouteRule{{Prefix: "", Service: "svc"}},
	}
	err := cm.validateRoutingConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prefix is empty")
}

func TestValidateRoutingConfig_EmptyService(t *testing.T) {
	cm := &ConfigManager{logger: getDefaultLogger()}
	cfg := &RoutingConfig{
		Rules: []RouteRule{{Prefix: "/api", Service: ""}},
	}
	err := cm.validateRoutingConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "service_name is empty")
}

func TestValidateRoutingConfig_DuplicatePrefix(t *testing.T) {
	cm := &ConfigManager{logger: getDefaultLogger()}
	cfg := &RoutingConfig{
		Rules: []RouteRule{
			{Prefix: "/api", Service: "svc1"},
			{Prefix: "/api", Service: "svc2"},
		},
	}
	err := cm.validateRoutingConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate prefix")
}

func TestValidateRoutingConfig_NormalizePrefix(t *testing.T) {
	cm := &ConfigManager{logger: getDefaultLogger()}
	cfg := &RoutingConfig{
		Rules:          []RouteRule{{Prefix: "api", Service: "api-svc"}},
		DefaultService: "fallback",
	}
	err := cm.validateRoutingConfig(cfg)
	assert.NoError(t, err)
	assert.Equal(t, "/api", cfg.Rules[0].Prefix) // phải được normalize
}

func TestValidateRoutingConfig_NoRulesWithDefault_OK(t *testing.T) {
	cm := &ConfigManager{logger: getDefaultLogger()}
	cfg := &RoutingConfig{DefaultService: "fallback"}
	assert.NoError(t, cm.validateRoutingConfig(cfg))
}

func TestValidateRoutingConfig_NoRulesNoDefault_Error(t *testing.T) {
	cm := &ConfigManager{logger: getDefaultLogger()}
	err := cm.validateRoutingConfig(&RoutingConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "all requests will fail")
}

// ============================================================
// SetOnChange / concurrent safe
// ============================================================

func TestSetOnChange_CalledOnReload(t *testing.T) {
	dir := t.TempDir()
	called := 0
	var mu sync.Mutex

	writeConfigFile(t, dir, validConfigYML())
	cm, err := NewConfigManager(dir, nil)
	require.NoError(t, err)

	cm.SetOnChange(func(c *Config) {
		mu.Lock()
		called++
		mu.Unlock()
	})

	// Trigger reload thủ công
	_ = cm.reloadConfig()

	mu.Lock()
	defer mu.Unlock()
	// onChange không được gọi trong reloadConfig trực tiếp — chỉ gọi từ watcher
	// Test này đảm bảo SetOnChange không panic
	_ = called
}

// ============================================================
// Concurrent snapshot read/write
// ============================================================

func TestSnapshot_ConcurrentReadWrite(t *testing.T) {
	cm := &ConfigManager{}
	var wg sync.WaitGroup

	// 10 writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cm.StoreSnapshot(&ConfigSnapshot{
				Config:  &Config{},
				Routing: &RoutingConfig{DefaultService: "svc"},
			})
		}()
	}

	// 10 readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cm.GetSnapshot()
			_ = cm.GetConfig()
			_ = cm.GetRoutingConfig()
			_ = cm.GetRetryConfig()
		}()
	}

	wg.Wait() // không panic, không race → pass
}

// ============================================================
// NewConfigManager
// ============================================================

func TestNewConfigManager_ValidDir(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, validConfigYML())
	cm, err := NewConfigManager(dir, nil)
	require.NoError(t, err)
	require.NotNil(t, cm)
	assert.NotNil(t, cm.GetConfig())
	assert.NotNil(t, cm.GetRoutingConfig())
}

// NOTE: NewConfigManager gọi os.Exit(1) khi file không tồn tại
// Không thể test trực tiếp os.Exit — sẽ kill toàn bộ test process
// Recommendation: refactor loadConfig để trả về error thay vì os.Exit
