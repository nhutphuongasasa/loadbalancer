package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

func (r *CacheClient) SetArray(ctx context.Context, key string, arr interface{}, ttl time.Duration) error {
	if arr == nil {
		return errors.New("arr cannot be nil")
	}
	if key == "" {
		return errors.New("key cannot be empty")
	}

	data, err := json.Marshal(arr)
	if err != nil {
		return fmt.Errorf("SetArray marshal: %w", err)
	}

	return r.client.Set(ctx, key, data, ttl).Err()
}

func (r *CacheClient) SetArrayNX(ctx context.Context, key string, arr interface{}, ttl time.Duration) (bool, error) {
	if arr == nil {
		return false, errors.New("arr cannot be nil")
	}

	data, err := json.Marshal(arr)
	if err != nil {
		return false, fmt.Errorf("SetArrayNX marshal: %w", err)
	}

	return r.client.SetNX(ctx, key, data, ttl).Result()
}

func (r *CacheClient) GetArray(ctx context.Context, key string, dest interface{}) error {
	if dest == nil {
		return errors.New("dest must be a non-nil pointer to slice")
	}
	if key == "" {
		return errors.New("key cannot be empty")
	}

	data, err := r.client.Get(ctx, key).Bytes()
	if err != nil {
		return err
	}

	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("GetArray unmarshal key=%q: %w", key, err)
	}

	return nil
}

func (r *CacheClient) DeleteKey(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

func (r *CacheClient) Exists(ctx context.Context, key string) (bool, error) {
	n, err := r.client.Exists(ctx, key).Result()
	return n > 0, err
}
