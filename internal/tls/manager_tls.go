package tls

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

type ManagerSTL struct {
	mux        sync.RWMutex
	certFile   string
	keyFile    string
	rootCAs    *x509.CertPool     //danh sach chung chi ma minh chap nhan tu client goi den
	config     *tls.Config        //chua thong tin chung chi dang dung
	clientAuth tls.ClientAuthType //cac che do kiem tra client

	logger *slog.Logger

	watcher    *fsnotify.Watcher
	done       chan struct{}
	reloadChan chan struct{}

	ctx      context.Context
	cancel   context.CancelFunc
	startOne sync.Once
	stopOne  sync.Once
	wg       sync.WaitGroup
}

type Option func(*ManagerSTL)

func WithClientAuth(CAPool *x509.CertPool, auth tls.ClientAuthType) Option {
	return func(m *ManagerSTL) {
		m.rootCAs = CAPool
		m.clientAuth = auth
	}
}

func WithLogger(logger *slog.Logger) Option {
	return func(m *ManagerSTL) {
		m.logger = logger
	}
}

func NewManagerSTL(certDir string, opts ...Option) (*ManagerSTL, error) {
	absDir, err := filepath.Abs(certDir)
	if err != nil {
		return nil, err
	}

	m := &ManagerSTL{
		certFile:   filepath.Join(absDir, "cert.pem"),
		keyFile:    filepath.Join(absDir, "key.pem"),
		clientAuth: tls.NoClientCert,
		logger:     slog.Default(),
		done:       make(chan struct{}),
		reloadChan: make(chan struct{}, 1),
	}

	for _, opt := range opts {
		opt(m)
	}

	m.logger.Info("TLS Manager initialized with auto-reload",
		"cert_file", m.certFile,
		"key_file", m.keyFile,
	)

	return m, nil
}

func (m *ManagerSTL) Reload() error {
	return m.reload()
}

func (m *ManagerSTL) reload() error {
	m.mux.Lock()
	defer m.mux.Unlock()

	//kierm tra cap key
	cert, err := tls.LoadX509KeyPair(m.certFile, m.keyFile)
	if err != nil {
		m.logger.Error("Failed to load TLS cert/key", "error", err, "cert", m.certFile, "key", m.keyFile)
		return err
	}

	newConfig := &tls.Config{
		Certificates: []tls.Certificate{cert}, // privatekey giup chung minh ra day chinh la server co domain nay
		MinVersion:   tls.VersionTLS12,
		// giao tiep cleint va chon laoi toan hoc ma hoa trao doi public va cung gen ra 1 cap key moi
		// no ma hoa data trong seqment
		CurvePreferences: []tls.CurveID{
			tls.X25519,
			tls.CurveP256,
		},
	}

	if m.rootCAs != nil {
		newConfig.ClientAuth = m.clientAuth
		newConfig.ClientCAs = m.rootCAs
	}

	m.config = newConfig

	m.logger.Info("TLS certificate reloaded successfully",
		"cert_file", m.certFile,
		"key_file", m.keyFile,
		"timestamp", time.Now().Format(time.RFC3339),
	)

	return nil
}

func (m *ManagerSTL) Start() error {
	var err error
	m.startOne.Do(func() {
		m.ctx, m.cancel = context.WithCancel(context.Background())
		m.wg.Add(1)
		//thuc hien kiem tra den dia chi ram co du lieu khong
		if err = m.reload(); err != nil {
			m.logger.Error("initial TLS load failed", "err", err)
			return
		}

		if err = m.startWatcher(); err != nil {
			m.logger.Error("failed to start fsnotify watcher", "err", err)
			return
		}
	})

	return err
}

func (m *ManagerSTL) Stop() {
	m.stopOne.Do(func() {
		if m.cancel != nil {
			m.logger.Info("Stopping watcher tls config")
			m.cancel()
		}
		if m.watcher != nil {
			m.wg.Wait()
		}
		m.logger.Info("Complete stop watcher tls config")
	})
}

func (m *ManagerSTL) startWatcher() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		m.logger.Error("Having error in init watcher", "error", err)
		return err
	}

	m.watcher = watcher

	dir := filepath.Dir(m.certFile)
	if err = watcher.Add(dir); err != nil {
		m.logger.Error("Failed to watch directory", "dir", dir, "error", err)
		return err
	}

	m.logger.Info("Started fsnotify watcher on directory", "dir", dir)

	go func() {
		defer m.wg.Done()
		for {
			select {
			case <-m.done:
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				if event.Has(fsnotify.Write) ||
					event.Has(fsnotify.Create) ||
					event.Has(fsnotify.Rename) ||
					event.Has(fsnotify.Remove) {
					if filepath.Base(event.Name) == filepath.Base(m.certFile) ||
						filepath.Base(event.Name) == filepath.Base(m.keyFile) {
						m.logger.Debug("Detected certificate file change", "event", event)

						select {
						case m.reloadChan <- struct{}{}:
						default:
						}
					}
				}
			case err, ok := <-watcher.Errors:
				//huy ham khi close
				if !ok {
					return
				}
				m.logger.Warn("fsnotify error", "error", err)
			case <-m.reloadChan:
				if err := m.reload(); err != nil {
					m.logger.Warn("Having error in reload config tls", "error", err)
				}
			case <-m.ctx.Done():
				m.watcher.Close()
				return
			}
		}
	}()

	return nil
}

/*
*giup nap lai config khong can khoi dong dua cho server phuong thuc thay vi ca doi tuong
 */
func (m *ManagerSTL) GetTLSConfig() *tls.Config {
	return &tls.Config{
		GetConfigForClient: func(chi *tls.ClientHelloInfo) (*tls.Config, error) {
			m.mux.RLock()
			defer m.mux.RUnlock()

			cfg := m.config.Clone()

			//tranh gap loi
			if m.rootCAs != nil {
				cfg.ClientCAs = m.rootCAs
				cfg.ClientAuth = m.clientAuth
			}

			return cfg, nil
		},
		MinVersion: tls.VersionTLS12,
		CurvePreferences: []tls.CurveID{
			tls.X25519,
			tls.CurveP256,
		},
	}
}

func LoadCAPool(caPath string) (*x509.CertPool, error) {
	caCert, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("could not read CA file: %w", err)
	}

	caCertPool := x509.NewCertPool()
	if ok := caCertPool.AppendCertsFromPEM(caCert); !ok {
		return nil, fmt.Errorf("failed to append CA cert to pool")
	}

	return caCertPool, nil
}
