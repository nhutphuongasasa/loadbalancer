package sticky

import (
	"encoding/json"
	"errors"
	"time"
)

// ServiceBinding luu thong tin mot diem dinh tuyen cu the:
// ten instance va thoi diem "dính" duoc tao ra.
type ServiceBinding struct {
	InstanceID string `json:"instance_id"`
	BoundAt    int64  `json:"bound_at"` // thoi diem bat dau vao 1 server cu the
}

// SessionPayload la "ban do dinh tuyen di dong" cua mot client.
// Moi service la mot key, value la instance cu the client dang "dinh" vao.
type SessionPayload struct {
	Bindings  map[string]ServiceBinding `json:"bindings"`
	ExpiresAt int64                     `json:"expires_at"`
}

var (
	ErrPayloadExpired  = errors.New("sticky: session payload expired")
	ErrPayloadInvalid  = errors.New("sticky: session payload invalid")
	ErrServiceNotFound = errors.New("sticky: service not found in session")
)

// newPayload tao mot SessionPayload moi voi mot binding khoi dau.
// Cac binding bo sung duoc them bang AddBinding().
func newPayload(serviceName, instanceID string, ttl time.Duration) (*SessionPayload, error) {
	if serviceName == "" || instanceID == "" {
		return nil, errors.New("sticky: serviceName and instanceID must not be empty")
	}
	p := &SessionPayload{
		Bindings:  make(map[string]ServiceBinding),
		ExpiresAt: time.Now().Add(ttl).UnixNano(),
	}
	p.Bindings[serviceName] = ServiceBinding{
		InstanceID: instanceID,
		BoundAt:    time.Now().UnixNano(),
	}
	return p, nil
}

// AddBinding them hoac cap nhat binding cho mot service.
// Chi lam moi BoundAt neu instanceID thay doi (tranh ghi du lieu thua).
func (p *SessionPayload) AddBinding(serviceName, instanceID string) error {
	if serviceName == "" || instanceID == "" {
		return errors.New("sticky: serviceName and instanceID must not be empty")
	}
	existing, ok := p.Bindings[serviceName]
	if ok && existing.InstanceID == instanceID {
		// Khong co gi thay doi, khong can ghi lai
		return nil
	}
	p.Bindings[serviceName] = ServiceBinding{
		InstanceID: instanceID,
		BoundAt:    time.Now().UnixNano(),
	}
	return nil
}

// RemoveBinding xoa binding cua mot service (dung khi instance bi chet).
func (p *SessionPayload) RemoveBinding(serviceName string) {
	delete(p.Bindings, serviceName)
}

// GetInstanceID tra ve instanceID cua mot service, kem flag cho biet co ton tai khong.
func (p *SessionPayload) GetInstanceID(serviceName string) (string, bool) {
	b, ok := p.Bindings[serviceName]
	if !ok {
		return "", false
	}
	return b.InstanceID, true
}

// HasBinding kiem tra nhanh mot service co trong map chua.
func (p *SessionPayload) HasBinding(serviceName string) bool {
	_, ok := p.Bindings[serviceName]
	return ok
}

// encodePayload chuyen payload sang JSON de ma hoa AES-GCM.
func (p *SessionPayload) encodePayload() ([]byte, error) {
	return json.Marshal(p)
}

// decodePayload giai ma tu byte sang SessionPayload.
func decodePayload(data []byte) (*SessionPayload, error) {
	var p SessionPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, ErrPayloadInvalid
	}
	if len(p.Bindings) == 0 {
		return nil, ErrPayloadInvalid
	}
	// Validate tung binding trong map
	for svc, b := range p.Bindings {
		if svc == "" || b.InstanceID == "" {
			return nil, ErrPayloadInvalid
		}
	}
	return &p, nil
}

// valid kiem tra payload da het han chua.
func (p *SessionPayload) valid() error {
	if time.Now().UnixNano() > p.ExpiresAt {
		return ErrPayloadExpired
	}
	return nil
}
