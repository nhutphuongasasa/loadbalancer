package utils

import (
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/nhutphuongasasa/loadbalancer/internal/config"
)

var (
	once     sync.Once
	instance *slog.Logger
)

func GetRootDir() string {
	_, filename, _, ok := runtime.Caller(0)
	if ok {
		return filepath.Dir(filepath.Dir(filepath.Dir(filename)))
	}

	exePath, err := os.Executable()
	if err != nil {
		dir, _ := os.Getwd()
		return dir
	}

	return filepath.Dir(exePath)
}

func GetLogger(cfg *config.ConfigManager) *slog.Logger {
	once.Do(func() {
		var level slog.Level
		switch strings.ToLower(cfg.GetConfig().LogConfig.Level) {
		case "debug":
			level = slog.LevelDebug
		case "warn":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		default:
			level = slog.LevelInfo
		}

		opts := &slog.HandlerOptions{
			Level:     level,
			AddSource: level == slog.LevelDebug,
		}

		var handler slog.Handler
		if strings.ToLower(cfg.GetConfig().LogConfig.Format) == "json" {
			handler = slog.NewJSONHandler(os.Stdout, opts)
		} else {
			handler = slog.NewTextHandler(os.Stdout, opts)
		}

		instance = slog.New(handler).With(
			slog.String("service", "load-balancer"),
		)
	})

	return instance
}
