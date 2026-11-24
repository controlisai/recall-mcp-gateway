package auth

import "context"

// HeadersProvider adds arbitrary static headers.
type HeadersProvider struct {
	headers map[string]string
}

func (p *HeadersProvider) Headers(ctx context.Context) map[string]string {
	cp := make(map[string]string, len(p.headers))
	for k, v := range p.headers {
		cp[k] = v
	}
	return cp
}

func (p *HeadersProvider) Name() string { return "headers" }
