package cache

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

func (r *CacheClient) SetString(ctx context.Context, key string, value string, expiration time.Duration) error {
	return r.client.Set(ctx, key, value, expiration).Err()
}

func (r *CacheClient) GetString(ctx context.Context, key string) (string, error) {
	val, err := r.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", nil
	}
	return val, err
}

func (r *CacheClient) SetStruct(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return r.client.Set(ctx, key, value, expiration).Err()
}

func (r *CacheClient) GetStruct(ctx context.Context, key string, dest interface{}) error {
	err := r.client.Get(ctx, key).Scan(dest)
	if err == redis.Nil {
		return nil
	}
	return err
}

func (r *CacheClient) Del(ctx context.Context, keys ...string) (int64, error) {
	return r.client.Del(ctx, keys...).Result()
}

func (r *CacheClient) Incr(ctx context.Context, key string) (int64, error) {
	return r.client.Incr(ctx, key).Result()
}

func (r *CacheClient) Expire(ctx context.Context, key string, expiration time.Duration) (bool, error) {
	return r.client.Expire(ctx, key, expiration).Result()
}

func (r *CacheClient) TTL(ctx context.Context, key string) (time.Duration, error) {
	return r.client.TTL(ctx, key).Result()
}

func (r *CacheClient) Exists(ctx context.Context, key string) (bool, error) {
	n, err := r.client.Exists(ctx, key).Result()
	return n == 1, err
}

func (r *CacheClient) SetArray(ctx context.Context, key string, arr interface{}, ttl time.Duration) error {
	if arr == nil {
		return errors.New("arr cannot be nil")
	}

	if !r.keyExists(ctx, key) {
		return r.setNewArray(ctx, key, arr, ttl)
	}

	return r.appendAndExtendTTL(ctx, key, arr)
}

func (r *CacheClient) keyExists(ctx context.Context, key string) bool {
	exists, err := r.Exists(ctx, key)
	return err == nil && exists
}

func (r *CacheClient) setNewArray(ctx context.Context, key string, arr interface{}, ttl time.Duration) error {
	return r.client.Set(ctx, key, arr, ttl).Err()
}

func (r *CacheClient) appendAndExtendTTL(ctx context.Context, key string, arr interface{}) error {
	current, err := r.getCurrentArray(ctx, key)
	if err != nil {
		return err
	}

	newItems, err := r.marshalToRawItems(arr)
	if err != nil {
		return err
	}

	current = append(current, newItems...)

	updated, err := json.Marshal(current)
	if err != nil {
		return err
	}

	pipe := r.client.Pipeline()
	pipe.Set(ctx, key, updated, 0)
	pipe.Expire(ctx, key, 15*time.Minute)
	_, err = pipe.Exec(ctx)

	return err
}

func (r *CacheClient) getCurrentArray(ctx context.Context, key string) ([]json.RawMessage, error) {
	var current []json.RawMessage
	err := r.client.Get(ctx, key).Scan(&current)
	if err == redis.Nil {
		return []json.RawMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	return current, nil
}

func (r *CacheClient) marshalToRawItems(arr interface{}) ([]json.RawMessage, error) {
	bytes, err := json.Marshal(arr)
	if err != nil {
		return nil, err
	}

	var items []json.RawMessage
	err = json.Unmarshal(bytes, &items)
	return items, err
}

func (r *CacheClient) GetArray(ctx context.Context, key string, dest interface{}) error {
	if dest == nil {
		return errors.New("dest must be a pointer to slice (e.g. &[]string)")
	}

	err := r.client.Get(ctx, key).Scan(dest)
	if err == redis.Nil {
		return nil
	}
	if err != nil {
		return err
	}
	return nil
}
