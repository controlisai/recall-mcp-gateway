package auth

import (
	"context"
	"encoding/base64"
)

// BasicProvider adds an Authorization: Basic header.
type BasicProvider struct {
	encoded string
}

// NewBasicProvider creates a basic auth provider.
func NewBasicProvider(username, password string) *BasicProvider {
	creds := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	return &BasicProvider{encoded: creds}
}

func (p *BasicProvider) Headers(ctx context.Context) map[string]string {
	return map[string]string{"Authorization": "Basic " + p.encoded}
}

func (p *BasicProvider) Name() string { return "basic" }
