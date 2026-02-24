package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/nhutphuongasasa/loadbalancer/internal/cache"
	"github.com/nhutphuongasasa/loadbalancer/internal/model"
	"github.com/redis/go-redis/v9"
)

type IStickier interface {
	Middleware(next http.Handler) http.Handler
	SetStickySession(w http.ResponseWriter, serverName, backendId string) error
	GetBackendFromContext(r *http.Request) ([]*model.ServerPair, bool)
	GetCacheKeyFromContext(r *http.Request) string
}

type stickyManager struct {
	cookieName string
	sessionTTL time.Duration
	logger     *slog.Logger
	cache      *cache.CacheClient
}

const (
	StickyCookieName  = "lb_sid"
	RedisKeyPrefix    = "lb:sticky:"
	DefaultSessionTTL = 3600 * time.Second
)

type contextKey string

const StickyBackendKey contextKey = "sticky_backend_id"
const CacheKey contextKey = "cache_key"

func NewStickyManager(logger *slog.Logger, cache *cache.CacheClient, ttl ...time.Duration) IStickier {
	sessionTTL := DefaultSessionTTL
	if len(ttl) > 0 && ttl[0] > 0 {
		sessionTTL = ttl[0]
	}

	return &stickyManager{
		cookieName: StickyCookieName,
		sessionTTL: sessionTTL,
		logger:     logger,
		cache:      cache,
	}
}

func (s *stickyManager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionID, err := s.getSessionIDFromCookie(r)
		if err != nil || sessionID == "" {
			next.ServeHTTP(w, r)
			return
		}

		serverPair, cacheKey, err := s.getBackendFromCache(r.Context(), sessionID)
		if err != nil {
			s.logger.Warn("Sticky session invalid or expired",
				"session_id", sessionID,
				"err", err,
			)
			http.SetCookie(w, &http.Cookie{
				Name:   s.cookieName,
				Value:  "",
				Path:   "/",
				MaxAge: -1,
			})
			next.ServeHTTP(w, r)
			return
		}

		ctx := context.WithValue(r.Context(), StickyBackendKey, serverPair)
		ctx = context.WithValue(ctx, CacheKey, cacheKey)

		r = r.WithContext(ctx)

		s.logger.Debug("Sticky session hit",
			"session_id", sessionID,
			"server_pair", serverPair,
		)

		next.ServeHTTP(w, r)
	})
}

/*
*Tao session luu vao cache set vao cookie
 */
func (s *stickyManager) SetStickySession(w http.ResponseWriter, serverName, backendId string) error {
	sessionID := generateSessionID()

	key := s.cacheKey(sessionID)

	ctx := context.Background()

	fields := []*model.ServerPair{
		{
			ServerName: serverName,
			InstanceId: backendId,
		},
	}

	if err := s.cache.SetArray(ctx, key, fields, s.sessionTTL); err != nil {
		return err
	}

	cookie := &http.Cookie{
		Name:     s.cookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(s.sessionTTL.Seconds()),
	}

	if w.Header().Get("X-Forwarded-Proto") == "https" {
		cookie.Secure = true
	}

	http.SetCookie(w, cookie)

	s.logger.Info("Created new sticky session",
		"session_id", sessionID,
		"backend_id", backendId,
		"ttl_seconds", s.sessionTTL.Seconds(),
	)

	return nil
}

func (s *stickyManager) GetBackendFromContext(r *http.Request) ([]*model.ServerPair, bool) {
	val := r.Context().Value(StickyBackendKey)
	if val == nil {
		return nil, false
	}
	serverPairs, ok := val.([]*model.ServerPair)
	if !ok {
		return nil, false
	}

	return serverPairs, true
}

func (s *stickyManager) cacheKey(sessionID string) string {
	return RedisKeyPrefix + sessionID
}

func (s *stickyManager) getSessionIDFromCookie(r *http.Request) (string, error) {
	cookie, err := r.Cookie(s.cookieName)
	if err == http.ErrNoCookie {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if cookie.Value == "" {
		return "", errors.New("empty session id")
	}
	return cookie.Value, nil
}

func (s *stickyManager) getBackendFromCache(ctx context.Context, sessionID string) ([]*model.ServerPair, string, error) {
	key := s.cacheKey(sessionID)
	var result []*model.ServerPair
	err := s.cache.GetArray(ctx, key, result)

	if err == redis.Nil {
		return nil, "", nil
	}

	if err != nil {
		return nil, "nil", nil
	}

	return result, key, nil
}

func generateSessionID() string {
	b := make([]byte, 16) // 128 bit
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *stickyManager) GetCacheKeyFromContext(r *http.Request) string {
	val := r.Context().Value(CacheKey)
	if val == nil {
		return ""
	}
	cacheKey, ok := val.(string)
	if !ok {
		return ""
	}

	return cacheKey
}
