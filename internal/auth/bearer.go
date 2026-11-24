package auth

import "context"

// BearerProvider adds an Authorization: Bearer header.
type BearerProvider struct {
	token string
}

func (p *BearerProvider) Headers(ctx context.Context) map[string]string {
	return map[string]string{"Authorization": "Bearer " + p.token}
}

func (p *BearerProvider) Name() string { return "bearer" }
