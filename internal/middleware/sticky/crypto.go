package sticky

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
)

var (
	ErrInvalidKeySize  = errors.New("aes-gcm: key must be 16, 24, or 32 bytes")
	ErrCiphertextShort = errors.New("aes-gcm: ciphertext too short")
	ErrDecryptFailed   = errors.New("aes-gcm: authentication failed or data corrupted")
)

type sessionCrypto struct {
	aead cipher.AEAD
}

// ma hoa aes-gcm gan authen tag vao tra ve aead de giai ma
func newSessionCrypto(key []byte) (*sessionCrypto, error) {
	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		return nil, ErrInvalidKeySize
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &sessionCrypto{aead: aead}, nil
}

// helper thuc hien tao nonce slide dung 1 lan  sau do dung readfull de tao o random
// dung seal cua aead de ma hoa plaintext bang nonce va key co san
// tiep theo da co ac block duoc  trao ky tu se ghep lai hoan chinh
//
//	roi tinh toan auth tag bang cach hash  ciphertext va nonce va extra data
//
// sau do dung nonce(dst) de lam prefix gan vao sealed
// ket qua co dnag nonce + ciphertext + auth tag
func (c *sessionCrypto) Encrypt(plaintext []byte) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, plaintext, nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// helper giai ma token  tra ve phain tetx duoi dnag byte
func (c *sessionCrypto) Decrypt(token string) ([]byte, error) {
	//decode chuyen sang byte de aes hoat dong
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, ErrDecryptFailed
	}
	//kiem tra do dai cua data
	//dung nonce size va do dai overload(auth tag) neu data nho hon thi du lieu co van de
	//binh thuong be nhat la 28byte(12 + 16)
	nonceSize := c.aead.NonceSize()
	if len(data) < nonceSize+c.aead.Overhead() {
		return nil, ErrCiphertextShort
	}
	//lay cac du lieu can thiet nonce, ciphertext
	//va lay plain text tu aead giai ma
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plain, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrDecryptFailed
	}
	return plain, nil
}
