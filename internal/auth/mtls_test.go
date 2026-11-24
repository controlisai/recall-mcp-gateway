package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMTLSProvider_Name(t *testing.T) {
	certPath, keyPath := generateTestCert(t)
	p, err := NewMTLSProvider(MTLSConfig{ClientCert: certPath, ClientKey: keyPath})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if p.Name() != "mtls" {
		t.Errorf("name = %q", p.Name())
	}
}

func TestMTLSProvider_HeadersReturnsNil(t *testing.T) {
	certPath, keyPath := generateTestCert(t)
	p, err := NewMTLSProvider(MTLSConfig{ClientCert: certPath, ClientKey: keyPath})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	h := p.Headers(context.Background())
	if h != nil {
		t.Errorf("headers = %v, want nil", h)
	}
}

func TestMTLSProvider_HTTPClient(t *testing.T) {
	certPath, keyPath := generateTestCert(t)
	p, err := NewMTLSProvider(MTLSConfig{ClientCert: certPath, ClientKey: keyPath})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	client := p.HTTPClient()
	if client == nil {
		t.Fatal("HTTPClient() returned nil")
	}
	if client.Transport == nil {
		t.Fatal("HTTPClient().Transport is nil")
	}
}

func TestMTLSProvider_ImplementsTransportProvider(t *testing.T) {
	certPath, keyPath := generateTestCert(t)
	p, err := NewMTLSProvider(MTLSConfig{ClientCert: certPath, ClientKey: keyPath})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	var _ TransportProvider = p // compile-time check
}

func TestMTLSProvider_InvalidCert(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "bad.pem")
	keyPath := filepath.Join(dir, "bad-key.pem")
	os.WriteFile(certPath, []byte("not a cert"), 0600)
	os.WriteFile(keyPath, []byte("not a key"), 0600)

	_, err := NewMTLSProvider(MTLSConfig{ClientCert: certPath, ClientKey: keyPath})
	if err == nil {
		t.Fatal("expected error for invalid cert/key")
	}
}

func TestMTLSProvider_MissingFile(t *testing.T) {
	_, err := NewMTLSProvider(MTLSConfig{ClientCert: "/nonexistent/cert.pem", ClientKey: "/nonexistent/key.pem"})
	if err == nil {
		t.Fatal("expected error for missing files")
	}
}

func TestNewProvider_MTLS(t *testing.T) {
	certPath, keyPath := generateTestCert(t)
	p, err := NewProvider(&Config{Type: "mtls", ClientCert: certPath, ClientKey: keyPath})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if p.Name() != "mtls" {
		t.Errorf("name = %q", p.Name())
	}
	tp, ok := p.(TransportProvider)
	if !ok {
		t.Fatal("expected TransportProvider")
	}
	if tp.HTTPClient() == nil {
		t.Fatal("HTTPClient() nil")
	}
}

func TestNewProvider_MTLS_RequiredFields(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{"missing client_cert", Config{Type: "mtls", ClientKey: "/tmp/k.pem"}},
		{"missing client_key", Config{Type: "mtls", ClientCert: "/tmp/c.pem"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewProvider(&tt.cfg)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

// generateTestCert creates a self-signed cert/key pair in a temp dir.
func generateTestCert(t *testing.T) (certPath, keyPath string) {
	t.Helper()
	dir := t.TempDir()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-client"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(1 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}

	certPath = filepath.Join(dir, "client.pem")
	certFile, _ := os.Create(certPath)
	pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	certFile.Close()

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPath = filepath.Join(dir, "client-key.pem")
	keyFile, _ := os.Create(keyPath)
	pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	keyFile.Close()

	return certPath, keyPath
}
