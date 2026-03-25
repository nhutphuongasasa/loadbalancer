package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/nhutphuongasasa/loadbalancer/internal/app"
	"github.com/nhutphuongasasa/loadbalancer/internal/utils"
)

func main() {
	rootDir := utils.GetRootDir()

	application, err := app.NewApp(rootDir)
	if err != nil {
		slog.Error("Error in init App", "error", err)
		os.Exit(1)
	}

	application.StartSubService()

	port := strconv.Itoa(application.GetConfigManager().GetConfig().Server.Port)

	publicServer := &http.Server{
		Addr:    ":" + port,
		Handler: application.GetHandler(),
		// TLSConfig:    application.GetTLSManager().GetTLSConfig(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	registryServer := &http.Server{
		Addr:    ":" + "8000",
		Handler: application.GetProviderServer().RegisterHTTPHandler(),
		// TLSConfig:    application.GetTLSManager().GetTLSConfig(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("Load balancer is starting", "port", port)
		printServerIPs("Public", port)
		if err := publicServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server error", "error", err)
			os.Exit(1)
		}
	}()

	go func() {
		slog.Info("Load registry server is starting", "port", "8000")
		if err := registryServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server error", "error", err)
			os.Exit(1)
		}
	}()

	//tao channel tin hieu bao dong
	quit := make(chan os.Signal, 1)
	//SIGINT la ctrl+c
	// SIGTERM la lenh tat may
	//khi xay tra trigger se day du lieu tin hieu vao channle khien cho no chay tiep caclenh phia duoi
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("Shutting down Load balancer")

	application.StopSubService()

	//huy theo timout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := publicServer.Shutdown(ctx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
	}

	slog.Info("Load Balancer exited gracefully")
}

func printServerIPs(name string, port string) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return
	}
	// Thêm tên Server để phân biệt Public hay Registry
	fmt.Printf("🌐 [%s] is listening on:\n", name)
	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				// Thêm dấu : giữa IP và Port
				fmt.Printf("   🔗 http://%s:%s\n", ipnet.IP.String(), port)
			}
		}
	}
}
