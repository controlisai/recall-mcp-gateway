package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/sanitize"
)

// Client fetches summary data from console-api for the embedded dashboard.
type Client struct {
	consoleAPIURL string
	serviceToken  string
	tenantID      string
	httpClient    *http.Client
}

// NewClient creates a platform client. Returns nil if not configured.
func NewClient(consoleAPIURL, serviceToken string) *Client {
	if consoleAPIURL == "" {
		return nil
	}
	return &Client{
		consoleAPIURL: consoleAPIURL,
		serviceToken:  serviceToken,
		httpClient:    &http.Client{Timeout: 5 * time.Second},
	}
}

// SetTenantID sets the tenant ID for API requests.
func (c *Client) SetTenantID(tenantID string) {
	c.tenantID = tenantID
}

// IsConfigured returns true if the platform client is set up.
func (c *Client) IsConfigured() bool {
	return c != nil && c.consoleAPIURL != ""
}

// FetchAgentHealth retrieves agent compliance data.
func (c *Client) FetchAgentHealth(ctx context.Context, productID string) ([]AgentHealth, error) {
	url := fmt.Sprintf("%s/api/v1/products/%s/agents", c.consoleAPIURL, productID)
	data, err := c.doGet(ctx, url)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Agents []AgentHealth `json:"agents"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		// Try array format
		var agents []AgentHealth
		if err2 := json.Unmarshal(data, &agents); err2 != nil {
			return nil, fmt.Errorf("parse agents: %w", err)
		}
		return agents, nil
	}
	return resp.Agents, nil
}

// FetchDriftEvents retrieves recent drift alerts.
func (c *Client) FetchDriftEvents(ctx context.Context, productID string) ([]DriftEvent, error) {
	url := fmt.Sprintf("%s/api/v1/products/%s/drift-events?limit=20", c.consoleAPIURL, productID)
	data, err := c.doGet(ctx, url)
	if err != nil {
		// Drift endpoint may not exist — not an error
		slog.Info("drift events unavailable", "component", "platform", "error", sanitize.Error(err.Error()))
		return nil, nil
	}

	var events []DriftEvent
	if err := json.Unmarshal(data, &events); err != nil {
		return nil, nil // graceful degradation
	}
	return events, nil
}

// FetchOrgSummary retrieves per-org decision statistics.
func (c *Client) FetchOrgSummary(ctx context.Context, productID string) ([]OrgSummary, error) {
	url := fmt.Sprintf("%s/api/v1/products/%s/orgs", c.consoleAPIURL, productID)
	data, err := c.doGet(ctx, url)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Orgs []OrgSummary `json:"orgs"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		var orgs []OrgSummary
		if err2 := json.Unmarshal(data, &orgs); err2 != nil {
			return nil, fmt.Errorf("parse orgs: %w", err)
		}
		return orgs, nil
	}
	return resp.Orgs, nil
}

// FetchSummary fetches all platform data for the dashboard.
func (c *Client) FetchSummary(ctx context.Context, productID, consoleURL string) *PlatformSummary {
	summary := &PlatformSummary{
		Connected:  true,
		ConsoleURL: consoleURL,
	}

	if agents, err := c.FetchAgentHealth(ctx, productID); err == nil {
		summary.Agents = agents
	}
	if drifts, err := c.FetchDriftEvents(ctx, productID); err == nil {
		summary.DriftEvents = drifts
	}
	if orgs, err := c.FetchOrgSummary(ctx, productID); err == nil {
		summary.Orgs = orgs
	}

	return summary
}

// UpdateConnection replaces the console-api URL, service token, and tenant ID at runtime.
func (c *Client) UpdateConnection(consoleAPIURL, serviceToken, tenantID string) {
	c.consoleAPIURL = consoleAPIURL
	c.serviceToken = serviceToken
	c.tenantID = tenantID
}

// ConsoleAPIURL returns the current console-api URL.
func (c *Client) ConsoleAPIURL() string {
	return c.consoleAPIURL
}

// FetchProducts retrieves the product list from console-api.
func (c *Client) FetchProducts(ctx context.Context) ([]ProductSummary, error) {
	url := fmt.Sprintf("%s/api/v1/products", c.consoleAPIURL)
	data, err := c.doGet(ctx, url)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Products []ProductSummary `json:"products"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		// Try array format
		var products []ProductSummary
		if err2 := json.Unmarshal(data, &products); err2 != nil {
			return nil, fmt.Errorf("parse products: %w", err)
		}
		return products, nil
	}
	return resp.Products, nil
}

// RegisterAgent creates an agent in the platform's agent registry.
func (c *Client) RegisterAgent(ctx context.Context, productID string, agent AgentRegistration) error {
	url := fmt.Sprintf("%s/api/v1/products/%s/agents", c.consoleAPIURL, productID)
	_, statusCode, err := c.doPost(ctx, url, agent)
	if err != nil {
		return err
	}
	if statusCode == http.StatusConflict {
		return nil // already exists — skip
	}
	return nil
}

// RegisterIntent creates an intent in the platform's intent registry.
func (c *Client) RegisterIntent(ctx context.Context, productID string, intent IntentRegistration) error {
	url := fmt.Sprintf("%s/api/v1/products/%s/intents", c.consoleAPIURL, productID)
	_, statusCode, err := c.doPost(ctx, url, intent)
	if err != nil {
		return err
	}
	if statusCode == http.StatusConflict {
		return nil // already exists — skip
	}
	return nil
}

// CreatePolicy creates an admissibility policy in the platform.
func (c *Client) CreatePolicy(ctx context.Context, productID string, policy PolicyRegistration) error {
	url := fmt.Sprintf("%s/api/v1/products/%s/policies", c.consoleAPIURL, productID)
	_, _, err := c.doPost(ctx, url, policy)
	return err
}

// PostMigrationSummary sends proxy baseline data to the platform for reference.
func (c *Client) PostMigrationSummary(ctx context.Context, productID string, summary MigrationSummary) error {
	url := fmt.Sprintf("%s/api/v1/products/%s/proxy-migration", c.consoleAPIURL, productID)
	_, _, err := c.doPost(ctx, url, summary)
	return err
}

func (c *Client) setAuthHeaders(req *http.Request) {
	if c.serviceToken != "" {
		req.Header.Set("X-Service-Token", c.serviceToken)
	}
	if c.tenantID != "" {
		req.Header.Set("X-Tenant-Id", c.tenantID)
	}
}

func (c *Client) doGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("platform request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("platform returned %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func (c *Client) doPost(ctx context.Context, url string, body any) ([]byte, int, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("platform request: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusConflict {
		return data, resp.StatusCode, fmt.Errorf("platform returned %d: %s", resp.StatusCode, string(data))
	}

	return data, resp.StatusCode, nil
}
