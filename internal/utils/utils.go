package utils

import (
	"fmt"
	"io"
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

func GetLogger(logCfg *config.LogConfig) *slog.Logger {
	once.Do(func() {
		var level slog.Level
		switch strings.ToLower(logCfg.Level) {
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

		root := GetRootDir()
		logPath := filepath.Join(root, "app.log")

		logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
		var output io.Writer = os.Stdout // Mặc định là Terminal

		if err == nil {
			output = io.MultiWriter(os.Stdout, logFile)
		} else {
			fmt.Printf("Warning: Could not open log file, logging to stdout only: %v\n", err)
		}

		var handler slog.Handler
		if strings.ToLower(logCfg.Format) == "json" {
			handler = slog.NewJSONHandler(output, opts)
		} else {
			handler = slog.NewTextHandler(output, opts)
		}

		instance = slog.New(handler).With(
			slog.String("service", "load balancer"),
		)
	})

	return instance
}
