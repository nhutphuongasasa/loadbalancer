package router

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"log/slog"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helper tao file cau hinh tam thoi de test
func setupTempConfigDir(t *testing.T, content string) (string, string) {
	//de go biet day la helper neu co loi l aloi cua ham goi ham nay
	t.Helper()

	tmpDir := t.TempDir() //tao thuc muc giup test
	configDir := filepath.Join(tmpDir, "config")
	require.NoError(t, os.Mkdir(configDir, 0755))

	configPath := filepath.Join(configDir, "routing.yml")
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0644))

	return tmpDir, configPath
}

func validConfigContent() string {
	return `rules:
  - prefix: /api/v1
    service_name: user-service
    strip_prefix: true
  - prefix: /api/v2
    service_name: payment-service
    strip_prefix: false
  - prefix: /public
    service_name: static-service
default_service: fallback-service`
}

func invalidConfigContent() string {
	return `rules:
  - prefix: /api`
}

func TestNewPathRouter_FileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	_, err := NewPathRouter(tmpDir, slog.Default())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config")
}

func TestNewPathRouter_Success(t *testing.T) {
	tmpDir, _ := setupTempConfigDir(t, validConfigContent())

	pr, err := NewPathRouter(tmpDir, slog.Default())
	require.NoError(t, err)
	require.NotNil(t, pr)

	assert.Equal(t, "fallback-service", pr.defaultSvc)
	assert.Len(t, pr.rules, 3)
}

func TestPathRouter_MatchService(t *testing.T) {
	tests := []struct {
		name       string
		rules      []RouteRule
		defaultSvc string
		path       string
		wantSvc    string
	}{
		{
			name: "match first rule",
			rules: []RouteRule{
				{Prefix: "/api/v1", Service: "svc1", StripPrefix: true},
				{Prefix: "/api", Service: "svc2"},
			},
			defaultSvc: "default",
			path:       "/api/v1/users",
			wantSvc:    "svc1",
		},
		{
			name: "match second rule (no prefix priority)",
			rules: []RouteRule{
				{Prefix: "/api", Service: "general"},
				{Prefix: "/api/special", Service: "special"},
			},
			defaultSvc: "fallback",
			path:       "/api/special/data",
			wantSvc:    "general",
		},
		{
			name:       "no match → default",
			rules:      []RouteRule{{Prefix: "/admin", Service: "admin"}},
			defaultSvc: "default-svc",
			path:       "/home",
			wantSvc:    "default-svc",
		},
		{
			name:       "empty path",
			rules:      []RouteRule{{Prefix: "/", Service: "root"}},
			defaultSvc: "fallback",
			path:       "",
			wantSvc:    "root",
		},
		{
			name:       "empty rules → default",
			rules:      nil,
			defaultSvc: "only-default",
			path:       "/anything",
			wantSvc:    "only-default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr := &PathRouter{
				rules:      tt.rules,
				defaultSvc: tt.defaultSvc,
				logger:     slog.Default(),
			}
			got := pr.MatchService(tt.path)
			assert.Equal(t, tt.wantSvc, got)
		})
	}
}

func TestPathRouter_GetStripPrefix(t *testing.T) {
	tests := []struct {
		name      string
		rules     []RouteRule
		path      string
		wantStrip bool
	}{
		{
			name: "strip true",
			rules: []RouteRule{
				{Prefix: "/api/v1", StripPrefix: true},
			},
			path:      "/api/v1/users",
			wantStrip: true,
		},
		{
			name: "strip false",
			rules: []RouteRule{
				{Prefix: "/api/v2", StripPrefix: false},
			},
			path:      "/api/v2/data",
			wantStrip: false,
		},
		{
			name:      "no match → false",
			rules:     []RouteRule{{Prefix: "/admin"}},
			path:      "/public",
			wantStrip: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr := &PathRouter{
				rules:  tt.rules,
				logger: slog.Default(),
			}
			got := pr.GetStripPrefix(tt.path)
			assert.Equal(t, tt.wantStrip, got)
		})
	}
}

func TestReloadConfig_Valid(t *testing.T) {
	tmpDir, _ := setupTempConfigDir(t, validConfigContent())

	pr, err := NewPathRouter(tmpDir, slog.Default())
	require.NoError(t, err)

	err = pr.reloadConfig()
	require.NoError(t, err)

	assert.Len(t, pr.rules, 3)
	assert.Equal(t, "fallback-service", pr.defaultSvc)
}

func TestReloadConfig_Invalid_KeepOld(t *testing.T) {
	tmpDir, configFile := setupTempConfigDir(t, validConfigContent())

	pr, err := NewPathRouter(tmpDir, slog.Default())
	require.NoError(t, err)

	oldRules := pr.rules
	oldDefault := pr.defaultSvc

	require.NoError(t, os.WriteFile(configFile, []byte(invalidConfigContent()), 0644))

	err = pr.reloadConfig()
	require.NoError(t, err)

	assert.Equal(t, oldRules, pr.rules)
	assert.Equal(t, oldDefault, pr.defaultSvc)
}

func TestReloadConfig_ENV_Override(t *testing.T) {
	tmpDir, _ := setupTempConfigDir(t, `
default_service: file-default
rules: []
`)

	t.Setenv("APP_DEFAULT_SERVICE", "env-override")

	pr, err := NewPathRouter(tmpDir, slog.Default())
	require.NoError(t, err)

	assert.Equal(t, "env-override", pr.defaultSvc)
}

func TestWatchConfig_ReloadOnChange(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping fsnotify integration test in short mode")
	}

	tmpDir, configFile := setupTempConfigDir(t, validConfigContent())

	pr, err := NewPathRouter(tmpDir, slog.Default())
	require.NoError(t, err)

	initialRulesCount := len(pr.rules)

	newContent := `
rules:
  - prefix: /new
    service_name: new-svc
default_service: new-default
`

	tmpFile := configFile + ".tmp"
	err = os.WriteFile(tmpFile, []byte(newContent), 0644)
	require.NoError(t, err)

	f, err := os.Open(tmpFile)
	require.NoError(t, err)
	require.NoError(t, f.Sync())
	f.Close()

	require.NoError(t, os.Rename(tmpFile, configFile))

	time.Sleep(100 * time.Millisecond)

	err = pr.reloadConfig()
	require.NoError(t, err)

	assert.Equal(t, 1, len(pr.rules))
	assert.Equal(t, "new-default", pr.defaultSvc)
	assert.Equal(t, "/new", pr.rules[0].Prefix)
	assert.Equal(t, "new-svc", pr.rules[0].Service)
	assert.NotEqual(t, initialRulesCount, len(pr.rules))
}
