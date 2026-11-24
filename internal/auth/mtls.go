package auth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"
)

// MTLSProvider authenticates via mutual TLS client certificates.
type MTLSProvider struct {
	httpClient *http.Client
	certPath   string
	keyPath    string
	caPath     string
	mu         sync.Mutex
}

// MTLSConfig configures the mTLS provider.
type MTLSConfig struct {
	ClientCert string // path to PEM client certificate
	ClientKey  string // path to PEM client private key
	CACert     string // optional: path to CA cert for verifying the server (empty = system roots)
}

// NewMTLSProvider creates an mTLS provider by loading the client cert/key pair.
func NewMTLSProvider(cfg MTLSConfig) (*MTLSProvider, error) {
	httpClient, err := buildMTLSClient(cfg.ClientCert, cfg.ClientKey, cfg.CACert)
	if err != nil {
		return nil, err
	}
	return &MTLSProvider{
		httpClient: httpClient,
		certPath:   cfg.ClientCert,
		keyPath:    cfg.ClientKey,
		caPath:     cfg.CACert,
	}, nil
}

func buildMTLSClient(certPath, keyPath, caPath string) (*http.Client, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("load client cert/key: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	// Load custom CA if provided (for self-signed server certs)
	if caPath != "" {
		caCert, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("read CA cert %s: %w", caPath, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA cert %s", caPath)
		}
		tlsConfig.RootCAs = pool
	}

	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}, nil
}

// Headers returns nil — mTLS operates at the transport level, not via headers.
func (p *MTLSProvider) Headers(ctx context.Context) map[string]string { return nil }

func (p *MTLSProvider) Name() string { return "mtls" }

// HTTPClient returns the http.Client configured with the client certificate.
func (p *MTLSProvider) HTTPClient() *http.Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.httpClient
}

// Reload re-reads the cert/key from disk. Supports cert rotation without restart.
func (p *MTLSProvider) Reload() error {
	httpClient, err := buildMTLSClient(p.certPath, p.keyPath, p.caPath)
	if err != nil {
		return fmt.Errorf("reload: %w", err)
	}

	p.mu.Lock()
	p.httpClient = httpClient
	p.mu.Unlock()

	slog.Info("mTLS cert reloaded", "component", "auth", "cert_path", p.certPath)
	return nil
}
