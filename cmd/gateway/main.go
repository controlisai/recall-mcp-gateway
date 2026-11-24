package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"sync/atomic"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"io"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/admin"
	"github.com/controlisai/recall-mcp-gateway/internal/alerts"
	"github.com/controlisai/recall-mcp-gateway/internal/approval"
	"github.com/controlisai/recall-mcp-gateway/internal/audit"
	proxyauth "github.com/controlisai/recall-mcp-gateway/internal/auth"
	proxyredis "github.com/controlisai/recall-mcp-gateway/internal/redis"
	"github.com/controlisai/recall-mcp-gateway/internal/baseline"
	"github.com/controlisai/recall-mcp-gateway/internal/config"
	"github.com/controlisai/recall-mcp-gateway/internal/dashboard"
	"github.com/controlisai/recall-mcp-gateway/internal/engine"
	"github.com/controlisai/recall-mcp-gateway/internal/platform"
	"github.com/controlisai/recall-mcp-gateway/internal/proxy"
	"github.com/controlisai/recall-mcp-gateway/internal/recommend"
	"github.com/controlisai/recall-mcp-gateway/internal/review"
	"github.com/controlisai/recall-mcp-gateway/internal/rules"
	"github.com/controlisai/recall-mcp-gateway/internal/store"
	"github.com/controlisai/recall-mcp-gateway/internal/tracing"
	"github.com/controlisai/recall-mcp-gateway/internal/upstream"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// version is set by -ldflags at build time
var version = "dev"

func main() {
	cfgPath := flag.String("config", "", "Path to configuration file")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("recall-mcp-gateway %s\n", version)
		os.Exit(0)
	}

	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// Initialize structured logging (before any slog calls)
	if v := os.Getenv("RECALL_LOG_LEVEL"); v != "" {
		cfg.Proxy.LogLevel = v
	}
	var logLevel slog.LevelVar
	switch strings.ToLower(cfg.Proxy.LogLevel) {
	case "debug":
		logLevel.Set(slog.LevelDebug)
	case "warn":
		logLevel.Set(slog.LevelWarn)
	case "error":
		logLevel.Set(slog.LevelError)
	default:
		logLevel.Set(slog.LevelInfo)
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: &logLevel})))

	// Env var overrides for config fields
	if v := os.Getenv("RECALL_DASHBOARD_PASS_HASH"); v != "" {
		cfg.Proxy.DashboardPassHash = v
	}
	if v := os.Getenv("RECALL_MAX_BODY_SIZE"); v != "" {
		cfg.Proxy.MaxBodySize = v
	}
	if v := os.Getenv("RECALL_RATE_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Proxy.RateLimit = n
		} else {
			slog.Warn("invalid env var", "var", "RECALL_RATE_LIMIT", "value", v)
		}
	}
	if v := os.Getenv("RECALL_METRICS_ENABLED"); v != "" {
		enabled := v == "true" || v == "1"
		cfg.Proxy.MetricsEnabled = &enabled
	}
	if v := os.Getenv("RECALL_SKIP_LOCAL_RULES"); v != "" {
		skip := v == "true" || v == "1"
		cfg.Proxy.SkipLocalRules = &skip
	}
	if v := os.Getenv("RECALL_PRODUCT"); v != "" {
		cfg.Proxy.Product = v
	}
	logAttrs := []any{"version", version}
	if cfg.Proxy.Product != "" {
		logAttrs = append(logAttrs, "product", cfg.Proxy.Product)
	}
	slog.Info("configuration", logAttrs...)

	// Initialize OpenTelemetry tracing (before any spans are created).
	// Also supports OTEL_EXPORTER_OTLP_ENDPOINT env var override.
	tracingEndpoint := cfg.Proxy.Tracing.Endpoint
	if v := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); v != "" {
		tracingEndpoint = v
	}
	serviceName := cfg.Proxy.Tracing.ServiceName
	if serviceName == "" {
		serviceName = "recall-mcp-gateway"
	}
	if cfg.Proxy.Tracing.Enabled || tracingEndpoint != "" {
		shutdownTracing, err := tracing.InitTracing(context.Background(), serviceName, tracingEndpoint)
		if err != nil {
			slog.Error("failed to initialize tracing", "error", err)
		} else {
			defer shutdownTracing(context.Background())
		}
	}

	// Gateway database — PostgreSQL (shared, multi-pod) or SQLite (standalone).
	// Opens FIRST so servers/agents/rules can be loaded from DB before wiring.
	var gwDB *store.GatewayDB
	var pgNotifier *store.Notifier
	if cfg.HasPostgres() {
		pgURL := os.ExpandEnv(cfg.Postgres.URL)
		gwDB, err = store.OpenPostgres(pgURL, cfg.Postgres.MaxConns)
		if err != nil {
			log.Fatalf("postgres: %v", err)
		}
		pgNotifier, err = store.NewNotifier(pgURL)
		if err != nil {
			log.Fatalf("pg notifier: %v", err)
		}
		defer pgNotifier.Close()
		slog.Info("gateway database ready", "backend", "postgres")
	} else {
		gwDBPath := filepath.Join(filepath.Dir(config.SavedConfigPath()), "gateway.db")
		gwDB, err = store.Open(gwDBPath)
		if err != nil {
			log.Fatalf("gateway database: %v", err)
		}
		slog.Info("gateway database ready", "backend", "sqlite", "path", gwDBPath)
	}
	defer gwDB.Close()

	// Seed empty tables from YAML config (first-run migration).
	// Once DB has data, YAML servers/agents/rules are ignored — DB is authoritative.
	gwDB.SeedFromConfig(cfg)

	// Load servers from DB (authoritative source)
	dbServers, err := gwDB.LoadServers()
	if err != nil {
		log.Fatalf("load servers from DB: %v", err)
	}
	slog.Info("servers loaded from DB", "count", len(dbServers))

	// Load agents from DB (authoritative source)
	dbAgents, err := gwDB.LoadAgents()
	if err != nil {
		log.Fatalf("load agents from DB: %v", err)
	}
	slog.Info("agents loaded from DB", "count", len(dbAgents))

	// Load rules from DB (authoritative source)
	dbRules, err := gwDB.LoadRules()
	if err != nil {
		log.Fatalf("load rules from DB: %v", err)
	}
	slog.Info("rules loaded from DB", "count", len(dbRules))

	// Connect to upstream MCP servers (with OAuth support)
	oauthConnector := upstream.NewOAuthConnector(cfg.Proxy.OAuthCallbackPort)
	pool := upstream.NewPool(dbServers, oauthConnector)

	// Wire per-server tool annotation overrides
	annotOverrides := make(map[string][]config.ToolAnnotationOverride)
	for _, s := range dbServers {
		if len(s.ToolAnnotations) > 0 {
			annotOverrides[s.Name] = s.ToolAnnotations
		}
	}
	if len(annotOverrides) > 0 {
		pool.Registry().SetAnnotationOverrides(annotOverrides)
		slog.Info("tool annotation overrides configured", "servers", len(annotOverrides))
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := pool.Connect(ctx); err != nil {
		log.Fatalf("upstream pool: %v", err)
	}

	// Audit store
	var auditor audit.AuditStore
	auditCfg := cfg.Proxy.Audit
	// Env var overrides
	if envPath := os.Getenv("RECALL_AUDIT_PATH"); envPath != "" {
		auditCfg.Path = envPath
	}
	if envRet := os.Getenv("RECALL_AUDIT_RETENTION"); envRet != "" {
		auditCfg.Retention = envRet
	}
	switch auditCfg.Storage {
	case "sqlite", "db":
		retention, err := config.ParseRetention(auditCfg.Retention)
		if err != nil {
			log.Fatalf("audit retention: %v", err)
		}
		if auditCfg.Path == "" {
			// No separate path — use shared gateway.db
			auditor, err = audit.NewSQLiteStoreWithDB(gwDB.DB(), audit.SQLiteConfig{
				Retention:  retention,
				MaxEntries: auditCfg.MaxEntries,
			})
			if err != nil {
				log.Fatalf("audit sqlite (gateway.db): %v", err)
			}
			slog.Info("audit configured", "storage", "sqlite", "path", "gateway.db (shared)", "retention", auditCfg.Retention)
		} else {
			auditor, err = audit.NewSQLiteStore(audit.SQLiteConfig{
				Path:       auditCfg.Path,
				Retention:  retention,
				MaxEntries: auditCfg.MaxEntries,
			})
			if err != nil {
				log.Fatalf("audit sqlite: %v", err)
			}
			slog.Info("audit configured", "storage", "sqlite", "path", auditCfg.Path, "retention", auditCfg.Retention)
		}
	case "jsonl":
		if auditCfg.Path != "" {
			auditor, err = audit.NewFileLogger(auditCfg.Path)
			if err != nil {
				log.Fatalf("audit log: %v", err)
			}
			slog.Info("audit configured", "storage", "jsonl", "path", auditCfg.Path)
		} else {
			auditor = audit.NewLogger(os.Stderr)
			slog.Info("audit configured", "storage", "jsonl", "path", "stderr")
		}
	case "memory":
		auditor = audit.NewLogger(os.Stderr)
		slog.Info("audit configured", "storage", "memory")
	default:
		slog.Warn("unknown audit storage, falling back to memory", "storage", auditCfg.Storage)
		auditor = audit.NewLogger(os.Stderr)
	}

	// Verify audit hash chain integrity on startup (last 100 entries)
	if recentEntries := auditor.RecentN(100); len(recentEntries) > 1 {
		if tampered := audit.VerifyChain(recentEntries); tampered >= 0 {
			slog.Warn("audit hash chain integrity check failed", "component", "audit", "tampered_at_index", tampered, "entries_checked", len(recentEntries))
		} else {
			slog.Info("audit hash chain verified", "component", "audit", "entries_checked", len(recentEntries))
		}
	}

	// Wire auditor to pool for persistent connection events
	pool.SetAuditor(auditor)
	pool.LoadEventsFromAudit()

	// Alert on persistent server failures (>5 min down) — log + audit entry
	pool.SetOnServerDown(func(serverName string, downSince time.Time) {
		slog.Warn("server down for >5 minutes", "component", "alert", "server", serverName, "downSince", downSince.Format("15:04:05"))
		auditor.Log(audit.Entry{
			Timestamp:     time.Now().UTC(),
			RequestID:     fmt.Sprintf("alert-%s-%d", serverName, time.Now().UnixMilli()),
			AgentID:       "system",
			Tool:          "(server-health)",
			Server:        serverName,
			Decision:      "block",
			DecisionReason: fmt.Sprintf("Server %s has been down for >5 minutes", serverName),
			IsError:       true,
			ErrorMessage:  fmt.Sprintf("persistent failure since %s", downSince.Format("15:04:05")),
			ErrorCategory: "network",
			Mode:          "health-check",
		})
	})

	// Agent auth — loaded from DB (authoritative source)
	auth := proxy.NewAgentAuthenticator(dbAgents)
	if auth.IsAnonymous() {
		slog.Info("auth configured", "mode", "anonymous")
	} else {
		slog.Info("auth configured", "agents", len(dbAgents))
	}

	// Rule engine — loaded from DB (authoritative source)
	ruleEngine := rules.NewEngine(dbRules)
	ruleEngine.SetPersistDB(gwDB.DB())
	if cfg.Proxy.DefaultAction != "" {
		ruleEngine.SetDefaultAction(cfg.Proxy.DefaultAction)
		slog.Info("default action configured", "action", cfg.Proxy.DefaultAction)
	}
	headerMode := cfg.Proxy.McpHeaderMode
	if headerMode == "" {
		headerMode = "passthrough"
	}
	slog.Info("mcp header mode", "mode", headerMode)
	pool.SetGlobalHeaderMode(headerMode)

	// Redis client (optional — for multi-pod shared state)
	var redisClient *proxyredis.Client
	if cfg.Redis != nil && cfg.Redis.URL != "" {
		var err error
		redisClient, err = proxyredis.NewClient(proxyredis.Config{
			URL:      os.ExpandEnv(cfg.Redis.URL),
			PoolSize: cfg.Redis.PoolSize,
			Password: os.ExpandEnv(cfg.Redis.Password),
			TLS:      cfg.Redis.TLS,
		})
		if err != nil {
			slog.Warn("redis connection failed, running in standalone mode", "error", err)
		} else {
			slog.Info("gateway mode", "mode", "cluster", "redis", cfg.Redis.URL)
			defer redisClient.Close()
		}
	}
	if redisClient == nil {
		slog.Info("gateway mode", "mode", "standalone")
	}

	// Cross-pod config sync via PG LISTEN/NOTIFY (multi-pod only).
	if pgNotifier != nil {
		ruleEngine.SetConfigNotifier(pgNotifier.For("rules"))
		slog.Info("config sync configured", "backend", "pg-notify")
	}
	// Redis: rate limiting only (multi-pod). SSE relay wired in dashboard section.
	if redisClient != nil {
		ruleEngine.SetRateLimiter(proxyredis.NewRedisRateLimiter(redisClient.Unwrap()))
		slog.Info("rate limiter configured", "storage", "redis")
	}

	approvalTTL, _ := config.ParseRetention(cfg.Proxy.Approval.TTL)
	if approvalTTL == 0 {
		approvalTTL = 5 * time.Minute
	}
	var approvalStore approval.StoreInterface
	switch cfg.Proxy.Approval.Storage {
	case "memory":
		approvalStore = approval.NewStore(approvalTTL)
		slog.Info("approval store configured", "storage", "memory", "ttl", approvalTTL)
	default: // "sqlite" or "" — persist by default for restart resilience
		approvalPath := cfg.Proxy.Approval.Path
		if approvalPath == "" {
			// No separate path — use shared gateway.db
			sqliteStore, err := approval.NewSQLiteStoreWithDB(gwDB.DB(), approvalTTL)
			if err != nil {
				slog.Warn("approval sqlite (gateway.db) failed, falling back to memory", "error", err)
				approvalStore = approval.NewStore(approvalTTL)
			} else {
				approvalStore = sqliteStore
				slog.Info("approval store configured", "storage", "sqlite", "path", "gateway.db (shared)", "ttl", approvalTTL)
			}
		} else {
			sqliteStore, err := approval.NewSQLiteStore(approval.SQLiteStoreConfig{
				Path: approvalPath,
				TTL:  approvalTTL,
			})
			if err != nil {
				slog.Warn("approval sqlite failed, falling back to memory", "error", err)
				approvalStore = approval.NewStore(approvalTTL)
			} else {
				approvalStore = sqliteStore
				slog.Info("approval store configured", "storage", "sqlite", "path", approvalPath, "ttl", approvalTTL)
			}
		}
	}
	reviewStore := review.NewDBStore(gwDB.DB())
	if pgNotifier != nil {
		reviewStore.SetConfigNotifier(pgNotifier.For("reviews"))
	}
	baseURL := fmt.Sprintf("http://localhost:%d", cfg.Proxy.Port)

	// Approval secret for requestState AEAD encryption
	approvalSecret := ""
	if cfg.AuthServer != nil {
		approvalSecret = cfg.AuthServer.ApprovalSecret
	}
	if cfg.HasExternalAS() && approvalSecret == "" {
		slog.Warn("approval_secret is empty — consent tokens use ephemeral key and will not survive restart",
			"component", "config")
	}
	if len(cfg.Proxy.AllowedOrigins) == 0 && cfg.HasExternalAS() {
		slog.Warn("allowed_origins is empty — all origins are allowed; configure for production",
			"component", "config")
	}

	// Token revocation store and JTI cache — declared here so Redis relay can reference them.
	// Both are always SQLite-backed (durable per pod); Redis pub/sub replicates mutations.
	var tokenRevocations *proxyauth.RevocationStore
	var jtiCache *proxyauth.JTICache

	// Auth server configuration: external AS (JWT validation) or built-in OAuth
	oauthServer := admin.NewOAuthServer(baseURL)
	if cfg.HasExternalAS() {
		// External AS mode: validate JWTs, PRM points to external issuer
		as := cfg.AuthServer
		jwksURL := as.JWKSURL
		if jwksURL == "" {
			jwksURL = strings.TrimRight(as.Issuer, "/") + "/.well-known/jwks.json"
		}
		jwksFetcher := proxyauth.NewJWKSFetcher(jwksURL)
		jwtValidator := proxyauth.NewJWTValidator(as.Issuer, as.Audience, jwksFetcher)
		if as.MaxDelegationDepth > 0 {
			jwtValidator.SetMaxDelegationDepth(as.MaxDelegationDepth)
		} else {
			slog.Warn("delegation depth unlimited — consider setting max_delegation_depth for production",
				"component", "auth", "issuer", as.Issuer)
		}
		// Revocation store — backed by shared gateway.db.
		// Redis pub/sub replicates revocations across pods (wired below after auth setup).
		tokenRevocations = proxyauth.NewRevocationStoreWithDB(gwDB.DB())
		jwtValidator.SetRevocationStore(tokenRevocations)
		slog.Info("revocation store configured", "storage", "gateway.db (shared)")
		// JTI cache — backed by shared gateway.db.
		// Redis pub/sub replicates JTI entries across pods (wired below after auth setup).
		jtiCache = proxyauth.NewJTICacheWithDB(gwDB.DB())
		jwtValidator.SetJTICache(jtiCache)
		slog.Info("jti cache configured", "storage", "gateway.db (shared)")
		auth.SetJWTValidator(jwtValidator)
		oauthServer.SetExternalAS(as.Issuer)
		oauthServer.SetJWTValidator(jwtValidator)
		// Token issuer for RFC 8693 token exchange (delegation)
		var tokenIssuer *proxyauth.TokenIssuer
		var tokenIssuerErr error
		if as.SigningKeyFile != "" {
			tokenIssuer, tokenIssuerErr = proxyauth.NewTokenIssuerFromPEM(baseURL, as.SigningKeyFile)
			if tokenIssuerErr == nil {
				slog.Info("token exchange enabled", "component", "oauth", "issuer", baseURL, "key", "persistent (PEM)")
			} else {
				slog.Warn("failed to load signing key from PEM, falling back to ephemeral", "error", tokenIssuerErr)
			}
		}
		if tokenIssuer == nil {
			tokenIssuer, tokenIssuerErr = proxyauth.NewTokenIssuer(baseURL)
			if tokenIssuerErr == nil {
				slog.Info("token exchange enabled", "component", "oauth", "issuer", baseURL, "key", "ephemeral (regenerated on restart)")
			}
		}
		if tokenIssuer != nil {
			oauthServer.SetTokenIssuer(tokenIssuer)
		}
		slog.Info("auth configured", "mode", "external-as", "issuer", as.Issuer, "audience", as.Audience)
	} else {
		// Built-in OAuth AS mode
		if gwDB != nil {
			oauthServer.SetPersistDB(gwDB.DB())
		} else {
			oauthTokensPath := filepath.Join(filepath.Dir(config.SavedConfigPath()), "oauth-tokens.json")
			oauthServer.SetPersistPath(oauthTokensPath)
		}
		auth.SetOAuthValidator(oauthServer)
	}
	approvalNotif := approval.NewWebhookNotifier("", baseURL)

	// Cross-pod security state sync (revocations, JTI) via PG LISTEN/NOTIFY.
	if pgNotifier != nil && cfg.HasExternalAS() {
		if tokenRevocations != nil {
			tokenRevocations.SetOnRevoke(func(hash string, expiresAt time.Time) {
				pgNotifier.Publish(ctx, "revocations") //nolint:errcheck
			})
		}
		if jtiCache != nil {
			jtiCache.SetOnRecord(func(jti string, expiresAt time.Time) {
				pgNotifier.Publish(ctx, "jti") //nolint:errcheck
			})
		}
		slog.Info("security state sync configured", "backend", "pg-notify")
	}

	// Baseline tracker — load persisted state from gateway.db if available.
	tracker := baseline.NewTracker()
	latencyTracker := baseline.NewLatencyTracker()
	if state, err := baseline.LoadStateFromDB(gwDB.DB()); err == nil && state != nil {
		tracker.SetBaselines(state.Baselines)
		latencyTracker.SetServers(state.ServerHealth)
	} else if err != nil {
		slog.Warn("failed to load baseline state from gateway.db", "component", "baseline", "error", err)
	}

	// Apply baseline defaults for servers with server_type configured.
	// Gives anomaly detection behavioral expectations from minute one.
	for _, sc := range dbServers {
		if sc.ServerType == "" {
			continue
		}
		profile := baseline.GetProfile(sc.ServerType)
		if profile == nil {
			slog.Warn("unknown server_type for baseline profile", "server", sc.Name, "server_type", sc.ServerType)
			continue
		}
		// Get tool names for this server from the registry
		serverTools := pool.Registry().ServerTools(sc.Name)
		toolNames := make([]string, len(serverTools))
		for i, t := range serverTools {
			toolNames[i] = t.Name
		}
		// Apply defaults for a wildcard agent "*" — applies to any agent using these tools
		if n := tracker.ApplyDefaults("*", toolNames, profile); n > 0 {
			slog.Info("applied baseline defaults", "server", sc.Name, "profile", sc.ServerType, "tools", n)
		}
	}

	// Alert detector + webhook
	alertDetector := alerts.NewDetector(tracker)
	alertWebhookURL := ""
	if len(cfg.Alerts) > 0 && cfg.Alerts[0].Notify != "" {
		alertWebhookURL = cfg.Alerts[0].Notify
	}
	alertWebhook := alerts.NewWebhookSender(alertWebhookURL, baseURL+"/dashboard")

	// Engine bridge (connected mode)
	var bridge *engine.Bridge
	if cfg.IsConnectedMode() {
		bridge = engine.NewBridge(engine.BridgeConfig{
			EngineURL:                cfg.Proxy.EngineURL,
			ConsoleAPIURL:            cfg.Proxy.ConsoleAPIURL,
			APIKey:                   cfg.Proxy.APIKey,
			OrgID:                    cfg.Proxy.DefaultOrgID,
			BlockOnEngineUnavailable: false,
			AutoRegister:             true,
		})
		slog.Info("engine connected", "url", cfg.Proxy.EngineURL)
	}

	// Platform client (connected mode)
	platformClient := platform.NewClient(cfg.Proxy.ConsoleAPIURL, cfg.Proxy.ServiceToken)

	// Create MCP proxy server — wires ALL components into the hot path
	proxyServer := proxy.NewServer(proxy.ServerConfig{
		Pool:          pool,
		Auth:          auth,
		Auditor:       auditor,
		Servers:       dbServers,
		Rules:         dbRules,
		RuleEngine:    ruleEngine,
		Agents:        dbAgents,
		Products:      cfg.Products,
		ApprovalStore:  approvalStore,
		ApprovalNotif:  approvalNotif,
		BaseURL:        baseURL,
		ApprovalSecret: approvalSecret,
		EngineBridge:  bridge,
		DefaultOrgID:  cfg.Proxy.DefaultOrgID,
		Tracker:       tracker,
		Latency:       latencyTracker,
		AlertDetector: alertDetector,
		AlertWebhook:  alertWebhook,
		SkipLocalRules: cfg.ShouldSkipLocalRules(),
		ClientTTLMs:    cfg.Proxy.ClientTTLMs,
	})

	// Wire upstream tool change notifications → proxy tool refresh.
	// When a modern upstream server sends notifications/tools/list_changed,
	// the pool triggers RefreshTools which re-registers tools with mcp-go
	// (which in turn notifies subscribed clients automatically).
	pool.SetOnToolsChanged(func(serverName string) {
		slog.Info("upstream tool change detected, refreshing tools", "component", "pool", "server", serverName)
		proxyServer.RefreshTools()
	})

	// Re-profile tools whenever the registry is rebuilt (connect, reconnect, tool change)
	pool.SetOnRegistryRebuilt(func() {
		for _, status := range pool.Statuses() {
			tools := pool.Registry().ServerTools(status.Name)
			proxyServer.Profiler().ProfileTools(status.Name, tools)
		}
	})

	// Start baseline persistence (save every 5 min + on shutdown) using shared gateway.db.
	stopPersist := make(chan struct{})
	baseline.StartPeriodicSaveToDB(gwDB.DB(), tracker, latencyTracker, 5*time.Minute, stopPersist)

	// Recommendation engine (+ optional LLM)
	recEngine := recommend.NewEngine(proxyServer.Profiler(), tracker, ruleEngine)
	if cfg.LLM != nil && cfg.LLM.Enabled {
		llmRec := recommend.NewLLMRecommender(*cfg.LLM)
		if llmRec != nil {
			recEngine.SetLLM(llmRec)
			slog.Info("recommendations LLM enabled", "provider", cfg.LLM.Provider, "model", cfg.LLM.Model)
		}
	}

	// Periodic recommendation generation
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				recent := auditor.Recent()
				if len(recent) == 0 {
					continue
				}
				// Build signal summaries from recent entries
				sigCounts := make(map[string]map[string]int) // category:tool -> count
				for _, entry := range recent {
					for _, sig := range entry.Signals {
						key := sig.Category + ":" + entry.Tool
						if sigCounts[key] == nil {
							sigCounts[key] = map[string]int{"count": 0}
						}
						sigCounts[key]["count"]++
					}
				}
				var signals []recommend.SignalSummary
				seen := make(map[string]bool)
				for _, entry := range recent {
					for _, sig := range entry.Signals {
						key := sig.Category + ":" + entry.Tool
						if seen[key] {
							continue
						}
						seen[key] = true
						signals = append(signals, recommend.SignalSummary{
							Category: sig.Category,
							Tool:     entry.Tool,
							Count:    sigCounts[key]["count"],
						})
					}
				}
				recEngine.Generate(signals)
			}
		}
	}()

	// HTTP mux
	mux := http.NewServeMux()
	if cfg.HasExternalAS() {
		// External AS: only register PRM endpoint (no /register, /authorize, /token)
		oauthServer.RegisterPRM(mux)
	} else {
		// Built-in OAuth: register all OAuth endpoints
		oauthServer.RegisterRoutes(mux)
	}
	mux.Handle("/healthz", admin.HealthHandler())
	mux.Handle("/readyz", admin.ReadyHandler(pool.IsReady))
	if cfg.IsMetricsEnabled() {
		mux.Handle("/metrics", promhttp.Handler())
		slog.Info("metrics endpoint enabled", "path", "/metrics")
	}

	// Admin APIs
	admin.NewAuditHandlers(auditor).RegisterRoutes(mux)
	approvalHandlers := admin.NewApprovalHandlers(approvalStore, cfg.Proxy.Approval.AdminKey)
	approvalHandlers.TrustedProxies = admin.ParseTrustedProxies(cfg.Proxy.TrustedProxies)
	if approvalSecret != "" && baseURL != "" {
		approvalHandlers.SetSigner(approval.NewSignedURLGenerator(approvalSecret, baseURL))
	}
	approvalHandlers.RegisterRoutes(mux)
	if tokenRevocations != nil {
		admin.NewRevocationHandler(tokenRevocations, cfg.Proxy.Approval.AdminKey).RegisterRoutes(mux)
	}
	if cfg.Proxy.Approval.SlackSigningSecret != "" {
		admin.NewSlackHandler(approvalStore, cfg.Proxy.Approval.SlackSigningSecret).RegisterRoutes(mux)
		slog.Info("slack callback enabled", "endpoint", "/slack/callback")
	}
	admin.NewReviewHandlers(reviewStore).RegisterRoutes(mux)
	admin.NewWebhookHandlers(approvalNotif, alertWebhook).RegisterRoutes(mux)
	admin.NewRecommendHandlers(recEngine, ruleEngine).RegisterRoutes(mux)
	rulesHandlers := admin.NewRulesHandlers(ruleEngine, proxyServer.Profiler(), cfg.Proxy.Approval.AdminKey)
	rulesHandlers.RegisterRoutes(mux)
	platformHandlers := admin.NewPlatformHandlers(platformClient, "", cfg.Proxy.ConsoleAPIURL)
	platformHandlers.SetOnConnect(func(params admin.ConnectParams) {
		slog.Info("platform connected", "component", "platform", "console", params.ConsoleAPIURL)

		// Wire engine bridge for two-layer evaluation
		if params.EngineURL != "" {
			newBridge := engine.NewBridge(engine.BridgeConfig{
				EngineURL:                params.EngineURL,
				ConsoleAPIURL:            params.ConsoleAPIURL,
				APIKey:                   params.APIKey,
				OrgID:                    params.DefaultOrgID,
				BlockOnEngineUnavailable: false,
				AutoRegister:             true,
			})
			proxyServer.SetEngineBridge(newBridge)
			slog.Info("engine bridge wired", "component", "platform", "url", params.EngineURL)
		}
	})
	platformHandlers.RegisterRoutes(mux)
	admin.NewWorkflowHandlers(proxyServer.WorkflowDetector(), proxyServer.History()).RegisterRoutes(mux)
	serverMgmt := admin.NewServerManagementHandlers(pool, gwDB, proxyServer.RefreshTools)
	if pgNotifier != nil {
		serverMgmt.SetConfigNotifier(pgNotifier.For("servers"))
	}
	serverMgmt.RegisterRoutes(mux)
	admin.NewBaselineHandlers(tracker).RegisterRoutes(mux)
	admin.NewSequenceHandlers(auditor).RegisterRoutes(mux)
	admin.NewExportHandlers(auditor).RegisterRoutes(mux)
	// Runtime config handler — hot-reload header mode, approval TTL, origins
	configHandlers := admin.NewConfigHandlers(admin.ConfigHandlersConfig{
		ProxyServer:   proxyServer,
		Pool:          pool,
		ApprovalStore: approvalStore,
	})
	configHandlers.RegisterRoutes(mux)
	auditPersistent := cfg.Proxy.Audit.Storage == "sqlite" || cfg.Proxy.Audit.Storage == "jsonl"
	metricsOn := cfg.Proxy.MetricsEnabled == nil || *cfg.Proxy.MetricsEnabled // default true
	tracingOn := cfg.Proxy.Tracing.Enabled // only true when explicitly enabled, not just endpoint present

	// InsightsDeps — AgentProvider set below after agentHandlers creation
	var insightsAgentProvider admin.AgentProvider
	agentHandlers := admin.NewAgentHandlers(dbAgents, func(agents []config.AgentConfig) {
		proxyServer.UpdateToolFilter(agents)
		auth.Swap(agents) // update token→agent mapping
	})
	// Enable agent persistence using shared DB.
	agentHandlers.SetPersistDB(gwDB.DB())
	if pgNotifier != nil {
		agentHandlers.SetConfigNotifier(pgNotifier.For("agents"))
	}
	// Re-sync auth after loading persisted agents
	auth.Swap(agentHandlers.Agents())
	// Wire agent provider so recommendation engine and insights consider blockedTools
	recEngine.SetAgentProvider(agentHandlers.Agents)
	insightsAgentProvider = agentHandlers.Agents
	mux.Handle("/admin/v1/insights", admin.InsightsHandler(admin.InsightsDeps{
		Tracker: tracker, Profiler: proxyServer.Profiler(),
		RuleEngine: ruleEngine, Auditor: auditor,
		ReviewStore:     reviewStore,
		AgentProvider:   insightsAgentProvider,
		AuditPersistent: auditPersistent,
		MetricsEnabled:  metricsOn,
		TracingEnabled:  tracingOn,
	}))
	agentHandlers.RegisterRoutes(mux)

	// Subscribe to config changes from other pods.
	// PG mode: LISTEN/NOTIFY — reload from shared DB on each notification.
	// Redis mode: pub/sub — apply full JSON payload directly.
	if pgNotifier != nil {
		go func() {
			changeCh := pgNotifier.Subscribe(ctx)
			for {
				select {
				case <-ctx.Done():
					return
				case change, ok := <-changeCh:
					if !ok {
						return
					}
					switch change.Type {
					case "servers":
						if servers, err := gwDB.LoadServers(); err == nil {
							// Diff: add new servers, remove deleted ones
							current := make(map[string]bool)
							for _, s := range pool.Configs() {
								current[s.Name] = true
							}
							incoming := make(map[string]bool)
							for _, s := range servers {
								incoming[s.Name] = true
								if !current[s.Name] {
									pool.AddServer(ctx, s) //nolint:errcheck
								}
							}
							for name := range current {
								if !incoming[name] {
									pool.RemoveServer(name) //nolint:errcheck
								}
							}
							proxyServer.RefreshTools()
							slog.Info("servers synced from peer pod", "component", "pg-notify", "count", len(servers))
						}
					case "rules":
						if rules, err := gwDB.LoadRules(); err == nil {
							ruleEngine.SetRules(rules)
							slog.Info("rules synced from peer pod", "component", "pg-notify", "count", len(rules))
						}
					case "agents":
						if agents, err := gwDB.LoadAgents(); err == nil {
							agentHandlers.ReplaceAll(agents)
							auth.Swap(agents)
							slog.Info("agents synced from peer pod", "component", "pg-notify", "count", len(agents))
						}
					case "reviews":
						reviewStore.ReloadFromDB()
						slog.Info("reviews synced from peer pod", "component", "pg-notify")
					case "revocations":
						if tokenRevocations != nil {
							tokenRevocations.ReloadFromDB()
						}
					case "jti":
						if jtiCache != nil {
							jtiCache.ReloadFromDB()
						}
					}
				}
			}
		}()
		slog.Info("config sync subscriber started", "component", "pg-notify")
	}

	// Periodic cleanup: prune stale baselines and auto-discovered agents (every 24h)
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pruneAge := 7 * 24 * time.Hour
				if n := tracker.PruneStale(pruneAge); n > 0 {
					slog.Info("pruned stale baselines", "count", n, "maxAge", "7d")
				}
				if n := agentHandlers.PruneAutoDiscovered(pruneAge); n > 0 {
					slog.Info("pruned stale auto-discovered agents", "count", n, "maxAge", "7d")
				}
			}
		}
	}()

	// Sync handler (platform migration)
	admin.NewSyncHandlers(admin.SyncDeps{
		PlatformHandlers: platformHandlers,
		AgentHandlers:    agentHandlers,
		Profiler:         proxyServer.Profiler(),
		RuleEngine:       ruleEngine,
		Tracker:          tracker,
		Latency:          latencyTracker,
		Version:          version,
	}).RegisterRoutes(mux)
	// Phase 1: Auto-register agents discovered from MCP initialize
	// Skip auto-registration for anonymous/system agent IDs
	proxyServer.SetOnAgentDiscovered(func(agentID string) {
		if agentID == "" || agentID == "anonymous" || agentID == "mcp" {
			return
		}
		if agentHandlers.AutoRegister(agentID) {
			slog.Info("auto-registered agent", "component", "agents", "agentID", agentID)
		}
	})
	mux.Handle("/admin/v1/status", admin.StatusHandler(admin.StatusDeps{
		Pool: pool, RuleEngine: ruleEngine, Tracker: tracker,
		Latency: latencyTracker, AlertDetector: alertDetector, AgentCount: len(dbAgents),
	}))
	mux.Handle("/admin/v1/reload", admin.ReloadHandler(admin.ReloadDeps{
		ConfigPath: *cfgPath, RuleEngine: ruleEngine,
		OnReload: func(newCfg *config.Config) {
			// Reload re-reads infrastructure config (proxy settings, auth_server, etc.).
			// Agents and rules are DB-only — reload doesn't affect them.
			// Note: auth_server changes (JWT validator, PRM issuer) are NOT re-wired on reload.
			// Changing auth_server requires a restart.
			slog.Info("config reloaded", "component", "reload")
		},
	}))
	replayHandler := admin.ReplayHandler(admin.ReplayDeps{Pool: pool, Auditor: auditor})
	diffHandler := admin.DiffHandler(proxyServer.History())
	mux.HandleFunc("/admin/v1/calls/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/diff") {
			diffHandler(w, r)
		} else {
			replayHandler(w, r)
		}
	})
	mux.Handle("/admin/v1/export-config", admin.ExportConfigHandler(cfg.Proxy.Port))
	mux.HandleFunc("/admin/v1/servers/", admin.ReauthHandler(admin.ReauthDeps{
		Pool:        pool,
		OnReconnect: proxyServer.RefreshTools,
	}))

	// MCP server — wrap with X-Accel-Buffering: no to prevent nginx/reverse proxy
	// from buffering SSE responses (subscriptions/listen, streaming tool results).
	// Also intercept POST body to extract raw tool arguments BEFORE mcp-go parses them.
	// Workaround for mcp-go v1.3.0 bug where arguments are corrupted on MRTP retries.
	mcpHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Accel-Buffering", "no")
		if r.Method == http.MethodPost && r.Body != nil {
			bodyBytes, err := io.ReadAll(r.Body)
			if err == nil {
				r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				// Parse JSON-RPC id and tool arguments from raw body
				var msg struct {
					ID     any `json:"id"`
					Params struct {
						Arguments map[string]any `json:"arguments"`
					} `json:"params"`
				}
				if json.Unmarshal(bodyBytes, &msg) == nil && msg.Params.Arguments != nil {
					// Key by tool name — the tool handler looks up by "rawargs:<toolName>"
					// This caches the LATEST args for each tool, which is sufficient
					// because consent retries happen within seconds of the original call.
					if name, ok := msg.Params.Arguments["__name__"]; ok {
						_ = name // unused, name is in the params not arguments
					}
					var nameMsg struct {
						Params struct {
							Name string `json:"name"`
						} `json:"params"`
					}
					if json.Unmarshal(bodyBytes, &nameMsg) == nil && nameMsg.Params.Name != "" {
						proxy.StashRawArgs("rawargs:"+nameMsg.Params.Name, msg.Params.Arguments)
					}
				}
			}
		}
		proxyServer.Handler().ServeHTTP(w, r)
	})
	if cfg.HasExternalAS() {
		// Wrap with 401 challenge middleware for MCP spec OAuth discovery
		prmURL := baseURL + "/.well-known/oauth-protected-resource"
		mux.Handle("/mcp", authChallengeMiddleware(prmURL, mcpHandler))
	} else {
		mux.Handle("/mcp", mcpHandler)
	}

	// Dashboard (with optional basic auth)
	if cfg.Proxy.Dashboard {
		dash, err := dashboard.NewHandler(pool, auditor, proxyServer.Profiler(), latencyTracker, version, cfg.Proxy.DashboardUser, cfg.Proxy.DashboardPassHash, ctx)
		if err != nil {
			log.Fatalf("dashboard: %v", err)
		}
		dash.SetModeConfig(cfg.Proxy.DefaultAction, cfg.Proxy.McpHeaderMode)
		metricsOn := cfg.Proxy.MetricsEnabled == nil || *cfg.Proxy.MetricsEnabled
		dash.SetObservabilityConfig(cfg.Proxy.Tracing.Enabled, cfg.Proxy.Tracing.Endpoint, metricsOn)
		authMode := "built-in"
		authIssuer := ""
		if cfg.HasExternalAS() {
			authMode = "external"
			authIssuer = cfg.AuthServer.Issuer
		}
		dash.SetAuthConfig(authMode, authIssuer)
		if cfg.AuthServer != nil {
			dash.SetAuthServerDetails(cfg.AuthServer.Audience, cfg.AuthServer.JWKSURL, cfg.AuthServer.MaxDelegationDepth)
		}
		approvalSecretSet := cfg.AuthServer != nil && cfg.AuthServer.ApprovalSecret != ""
		slackSecretSet := cfg.Proxy.Approval.SlackSigningSecret != ""
		approvalTtl := "5m"
		if cfg.Proxy.Approval.TTL != "" {
			approvalTtl = cfg.Proxy.Approval.TTL
		}
		approvalStorage := "memory"
		if cfg.Proxy.Approval.Storage != "" {
			approvalStorage = cfg.Proxy.Approval.Storage
		}
		dash.SetSecurityConfig(approvalSecretSet, slackSecretSet, cfg.Proxy.AllowedOrigins, approvalTtl, approvalStorage)
		if redisClient != nil {
			dash.SetClusterMode(true, cfg.Redis.URL)
			dash.SetSSERelay(proxyredis.NewRedisSSERelay(redisClient.Unwrap()))
		}
		dash.RegisterRoutes(mux)
		slog.Info("dashboard enabled", "url", fmt.Sprintf("http://localhost:%d/dashboard/", cfg.Proxy.Port))
		// Wire dashboard session validation into admin handlers
		approvalHandlers.SessionValidator = dash.IsValidSession
		rulesHandlers.SetSessionValidator(dash.IsValidSession)
	}

	// Parse body size limit
	maxBodyBytes, err := config.ParseBodySize(cfg.Proxy.MaxBodySize)
	if err != nil {
		log.Fatalf("max_body_size: %v", err)
	}

	// Build middleware chain: recover → requestID → origin → body limit → rate limit → mux
	var handler http.Handler = mux
	if cfg.Proxy.RateLimit > 0 {
		rl := newRateLimiter(cfg.Proxy.RateLimit)
		handler = rateLimitMiddleware(rl, handler)
		slog.Info("rate limit enabled", "ratePerSecond", cfg.Proxy.RateLimit)
	}
	handler = bodyLimitMiddleware(maxBodyBytes, handler)
	origins := newOriginGuard(cfg.Proxy.AllowedOrigins)
	handler = originMiddleware(origins, handler)
	handler = requestIDMiddleware(handler)
	handler = recoverMiddleware(handler)

	// Wire runtime config callbacks now that dash + origins are available
	configHandlers.OriginsUpdater = func(newOrigins []string) {
		origins.SetOrigins(newOrigins)
	}

	addr := fmt.Sprintf(":%d", cfg.Proxy.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // SSE
		IdleTimeout:  120 * time.Second,
	}

	// TLS: enforce minimum version 1.2
	if cfg.Proxy.TLSCert != "" && cfg.Proxy.TLSKey != "" {
		srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	go func() {
		if cfg.Proxy.TLSCert != "" && cfg.Proxy.TLSKey != "" {
			slog.Info("listening", "addr", addr, "tls", "1.2+")
			if err := srv.ListenAndServeTLS(cfg.Proxy.TLSCert, cfg.Proxy.TLSKey); err != nil && err != http.ErrServerClosed {
				log.Fatalf("https server: %v", err)
			}
		} else {
			slog.Info("listening", "addr", addr)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("http server: %v", err)
			}
		}
	}()

	slog.Info("ready", "servers", len(dbServers), "tools", pool.ToolCount(), "agents", len(dbAgents))

	<-ctx.Done()
	slog.Info("shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	// Shut down HTTP server first (drains in-flight requests)
	if err := srv.Shutdown(shutdownCtx); err != nil && err.Error() != "context deadline exceeded" {
		slog.Error("shutdown error", "error", err)
	}
	// Drain pending webhook notifications before closing stores
	proxyServer.DrainWebhooks(5 * time.Second)
	pool.Close()
	approvalStore.Close()
	auditor.Close()

	// Save baselines AFTER all requests drained
	close(stopPersist)
	time.Sleep(100 * time.Millisecond) // allow final save goroutine to complete

	slog.Info("stopped")
}

// authChallengeMiddleware returns HTTP 401 with WWW-Authenticate header when
// a request to the MCP endpoint has no Bearer token. This enables MCP spec
// OAuth discovery: client receives 401 → reads PRM URL → discovers AS → authenticates.
func authChallengeMiddleware(prmURL string, next http.Handler) http.Handler {
	challenge := fmt.Sprintf(`Bearer resource_metadata="%s"`, prmURL)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			w.Header().Set("WWW-Authenticate", challenge)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized","error_description":"Bearer token required"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// recoverMiddleware catches panics in HTTP handlers and returns 500.
func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				slog.Error("panic recovered", "error", err)
			slog.Debug("panic stack trace", "stack", string(debug.Stack()))
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// requestIDMiddleware generates a unique request ID and threads it through context + response header.
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := generateRequestID()
		w.Header().Set("X-Request-Id", id)
		ctx := proxy.ContextWithRequestID(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func generateRequestID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// bodyLimitMiddleware limits request body size.
func bodyLimitMiddleware(maxBytes int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		}
		next.ServeHTTP(w, r)
	})
}

// originMiddleware validates the Origin header against an allow list.
// Empty allowedOrigins means allow all origins (no restriction).
// originGuard holds a hot-reloadable allowed origins set.
type originGuard struct {
	allowed atomic.Value // map[string]bool
}

func newOriginGuard(origins []string) *originGuard {
	g := &originGuard{}
	g.SetOrigins(origins)
	return g
}

func (g *originGuard) SetOrigins(origins []string) {
	m := make(map[string]bool, len(origins))
	for _, o := range origins {
		m[strings.TrimRight(o, "/")] = true
	}
	g.allowed.Store(m)
}

func (g *originGuard) isAllowed(origin string) bool {
	m, _ := g.allowed.Load().(map[string]bool)
	return m[strings.TrimRight(origin, "/")]
}

func (g *originGuard) isEmpty() bool {
	m, _ := g.allowed.Load().(map[string]bool)
	return len(m) == 0
}

func originMiddleware(guard *originGuard, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if guard.isEmpty() {
			next.ServeHTTP(w, r)
			return
		}
		// Handle CORS preflight
		if r.Method == http.MethodOptions {
			origin := r.Header.Get("Origin")
			if origin != "" && guard.isAllowed(origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Mcp-Method, Mcp-Name, MCP-Protocol-Version")
				w.Header().Set("Access-Control-Max-Age", "86400")
				w.Header().Set("Vary", "Origin")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		if !guard.isAllowed(origin) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"origin not allowed"}`))
			return
		}
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Origin", origin)
		next.ServeHTTP(w, r)
	})
}

// rateLimiter implements a per-IP token bucket rate limiter.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    int // tokens per second
}

type bucket struct {
	tokens    float64
	lastFill  time.Time
}

func newRateLimiter(ratePerSecond int) *rateLimiter {
	rl := &rateLimiter{
		buckets: make(map[string]*bucket),
		rate:    ratePerSecond,
	}
	// Cleanup stale IPs every 60s
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			rl.mu.Lock()
			cutoff := time.Now().Add(-2 * time.Minute)
			for ip, b := range rl.buckets {
				if b.lastFill.Before(cutoff) {
					delete(rl.buckets, ip)
				}
			}
			rl.mu.Unlock()
		}
	}()
	return rl
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, ok := rl.buckets[ip]
	if !ok {
		b = &bucket{tokens: float64(rl.rate), lastFill: now}
		rl.buckets[ip] = b
	}

	// Refill tokens based on elapsed time
	elapsed := now.Sub(b.lastFill).Seconds()
	b.tokens += elapsed * float64(rl.rate)
	if b.tokens > float64(rl.rate) {
		b.tokens = float64(rl.rate)
	}
	b.lastFill = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// rateLimitMiddleware returns HTTP 429 when rate exceeded.
func rateLimitMiddleware(rl *rateLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		if ip == "" {
			ip = r.RemoteAddr
		}
		if !rl.allow(ip) {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func loadConfig(path string) (*config.Config, error) {
	if path == "" {
		// No flag: try saved config, then config.yaml in cwd, then empty dashboard mode
		if config.HasSavedConfig() {
			fmt.Printf("loading saved config from %s\n", config.SavedConfigPath())
			return config.LoadSavedConfig()
		}
		if _, err := os.Stat("config.yaml"); err == nil {
			path = "config.yaml"
		} else {
			fmt.Printf("\n  Recall MCP Gateway %s\n", version)
			fmt.Printf("  MCP tool monitoring & control\n\n")
			fmt.Printf("  No servers configured. Get started:\n\n")
			fmt.Printf("  Option 1: Open the dashboard and add servers\n")
			fmt.Printf("    Dashboard: http://localhost:8090/dashboard\n\n")
			fmt.Printf("  Option 2: Provide a config file\n")
			fmt.Printf("    recall-mcp-gateway --config path/to/config.yaml\n\n")
			fmt.Printf("  Docker:\n")
			fmt.Printf("    docker run -v ./config.yaml:/app/config.yaml controlisai/recall-mcp-gateway --config /app/config.yaml\n\n")
			fmt.Printf("  Starting with empty config (dashboard-only mode)...\n\n")
			return &config.Config{
				Proxy: config.ProxyConfig{Port: 8090, Dashboard: true},
			}, nil
		}
	}
	return config.Load(path)
}
