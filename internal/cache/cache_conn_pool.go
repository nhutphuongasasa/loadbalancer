package cache

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/nhutphuongasasa/loadbalancer/internal/config"
	"github.com/redis/go-redis/v9"
)

type CacheClient struct {
	client *redis.Client
	config *config.CacheConfig
	mu     sync.Mutex
	closed bool
}

var (
	once     sync.Once
	instance *CacheClient
)

func NewCacheClient(cfg *config.CacheConfig) (*CacheClient, error) {
	if cfg == nil {
		return nil, fmt.Errorf("redis config is required")
	}

	client := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: max(2, cfg.PoolSize/4),
		DialTimeout:  cfg.Timeout,
		ReadTimeout:  cfg.Timeout,
		WriteTimeout: cfg.Timeout,
		PoolTimeout:  cfg.Timeout + 5*time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("cannot connect to redis: %w", err)
	}

	return &CacheClient{
		client: client,
		config: cfg,
	}, nil
}

func GetInstance(cfg *config.CacheConfig) *CacheClient {
	once.Do(func() {
		var err error
		instance, err = NewCacheClient(cfg)
		if err != nil {
			log.Fatalf("Failed to initialize Redis: %v", err)
		}
	})
	return instance
}

func (r *CacheClient) Client() *redis.Client {
	return r.client
}

func (r *CacheClient) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}

	r.closed = true
	return r.client.Close()
}
