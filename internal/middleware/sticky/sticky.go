package sticky

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/nhutphuongasasa/loadbalancer/internal/config"
)

// IStickier dinh nghia hop dong cua sticky session manager.
//
// Luong hoat dong tong quat:
//  1. Middleware doc cookie, giai ma, dua SessionPayload vao context.
//  2. LB doc payload qua GetSessionFromContext -> GetInstanceID(serviceName)
//     de biet nen dinh tuyen den instance nao.
//  3. Sau khi chon duoc instance, LB goi BindService de cap nhat / them binding.
//  4. LB goi FlushSession de ghi cookie moi vao response.
//  5. Neu instance chet, LB goi RemoveBinding roi FlushSession.
type IStickier interface {
	// Middleware giai ma cookie (neu co) va dua SessionPayload vao request context.
	Middleware(next http.Handler) http.Handler

	// GetSessionFromContext lay SessionPayload da duoc inject boi Middleware.
	GetSessionFromContext(r *http.Request) (*SessionPayload, bool)

	// BindService them hoac cap nhat binding cho mot service trong payload.
	// Neu payload chua ton tai (client moi), tu dong tao moi.
	// Tra ve payload da cap nhat de truyen vao FlushSession.
	BindService(existing *SessionPayload, serviceName, instanceID string) (*SessionPayload, error)

	// RemoveBinding xoa binding cua mot service (vi du khi instance bi chet).
	// Neu payload nil hoac service khong ton tai, ham tra ve payload goc khong doi.
	RemoveBinding(existing *SessionPayload, serviceName string) *SessionPayload

	// FlushSession ghi SessionPayload hien tai vao cookie cua response.
	// Goi sau moi thay doi payload de dam bao client luon co ban do dinh tuyen moi nhat.
	FlushSession(w http.ResponseWriter, payload *SessionPayload) error
}

type contextKey string

const sessionContextKey contextKey = "sticky_payload"

type stickyManager struct {
	cfg    *config.StickySessionConfig
	crypto *sessionCrypto
	log    *slog.Logger
}

// NewStickyManager khoi tao mot IStickier hoan toan stateless.
func NewStickyManager(cfg *config.StickySessionConfig, logger *slog.Logger) (IStickier, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.CookieName == "" {
		cfg.CookieName = config.DefaultStickySessionConfig().CookieName
	}
	if cfg.TTL <= 0 {
		cfg.TTL = config.DefaultStickySessionConfig().TTL
	}
	crypto, err := newSessionCrypto(cfg.EncryptionKey)
	if err != nil {
		return nil, err
	}
	return &stickyManager{cfg: cfg, crypto: crypto, log: logger}, nil
}

// Middleware doc cookie, giai ma, inject SessionPayload vao context.
// Neu cookie khong co hoac bi lam gia mao, request van duoc chuyen tiep
// (payload = nil trong context) de LB tu chon instance theo chinh sach mac dinh.
func (m *stickyManager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, err := m.readCookie(r)
		if err != nil || token == "" {
			next.ServeHTTP(w, r)
			return
		}

		payload, err := m.unseal(token)
		if err != nil {
			m.log.Warn("sticky: rejected token",
				"reason", err.Error(),
				"remote", r.RemoteAddr,
			)
			m.clearCookie(w)
			next.ServeHTTP(w, r)
			return
		}

		m.log.Debug("sticky: session hit",
			"bindings_count", len(payload.Bindings),
		)

		ctx := context.WithValue(r.Context(), sessionContextKey, payload)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetSessionFromContext lay payload da inject boi Middleware.
func (m *stickyManager) GetSessionFromContext(r *http.Request) (*SessionPayload, bool) {
	v := r.Context().Value(sessionContextKey)
	if v == nil {
		return nil, false
	}
	p, ok := v.(*SessionPayload)
	return p, ok && p != nil
}

// BindService them hoac cap nhat binding cho serviceName trong payload.
//
// Neu existing == nil (client chua co cookie), tao mot SessionPayload moi.
// Neu existing != nil, AddBinding se chi ghi lai neu instanceID thay doi.
//
// Vi SessionPayload la con tro, ham nay tra ve chinh doi tuong da sua
// de caller co the tiep tuc chain cac thao tac truoc khi FlushSession.
func (m *stickyManager) BindService(existing *SessionPayload, serviceName, instanceID string) (*SessionPayload, error) {
	if existing == nil {
		// Client moi, tao payload voi binding dau tien
		p, err := newPayload(serviceName, instanceID, m.cfg.TTL)
		if err != nil {
			return nil, err
		}
		m.log.Info("sticky: new session created",
			"service", serviceName,
			"instance", instanceID,
		)
		return p, nil
	}

	// Client cu, them / cap nhat binding
	if err := existing.AddBinding(serviceName, instanceID); err != nil {
		return nil, err
	}
	m.log.Debug("sticky: binding updated",
		"service", serviceName,
		"instance", instanceID,
	)
	return existing, nil
}

// RemoveBinding xoa binding cua serviceName khoi payload.
// Thuong dung khi health-check bao instance da chet:
//
//	payload = manager.RemoveBinding(payload, "order")
//	_ = manager.FlushSession(w, payload)
func (m *stickyManager) RemoveBinding(existing *SessionPayload, serviceName string) *SessionPayload {
	if existing == nil {
		return nil
	}
	existing.RemoveBinding(serviceName)
	m.log.Info("sticky: binding removed", "service", serviceName)
	return existing
}

// FlushSession ma hoa payload hien tai va ghi vao cookie cua response.
// Nen goi o cuoi moi request handler sau khi tat ca cac BindService da hoan tat.
func (m *stickyManager) FlushSession(w http.ResponseWriter, payload *SessionPayload) error {
	if payload == nil {
		return errors.New("sticky: cannot flush nil payload")
	}
	token, err := m.seal(payload)
	if err != nil {
		return err
	}
	m.setCookie(w, token)
	m.log.Debug("sticky: session flushed", "bindings_count", len(payload.Bindings))
	return nil
}

// helper thuc hien ma hoa aes-gcm
func (m *stickyManager) seal(p *SessionPayload) (string, error) {
	data, err := p.encodePayload()
	if err != nil {
		return "", err
	}
	return m.crypto.Encrypt(data)
}

// thuc hien gia ma aes-gcm
func (m *stickyManager) unseal(token string) (*SessionPayload, error) {
	data, err := m.crypto.Decrypt(token)
	if err != nil {
		return nil, err
	}
	p, err := decodePayload(data)
	if err != nil {
		return nil, err
	}
	//kiem tra exprie
	if err := p.valid(); err != nil {
		return nil, err
	}
	return p, nil
}

// helper doc cookie
func (m *stickyManager) readCookie(r *http.Request) (string, error) {
	c, err := r.Cookie(m.cfg.CookieName)
	if errors.Is(err, http.ErrNoCookie) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return c.Value, nil
}

// helper set cookie vao response
func (m *stickyManager) setCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     m.cfg.CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(m.cfg.TTL.Seconds()),
		Secure:   m.cfg.Secure,
	})
}

// helper xoa cookie khoi client
func (m *stickyManager) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:   m.cfg.CookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
}
