package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// ToolCallsTotal counts tool call decisions.
	ToolCallsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "recall_proxy_tool_calls_total",
		Help: "Total tool calls by outcome",
	}, []string{"tool", "agent", "decision", "server"})

	// ToolLatency tracks tool call duration.
	ToolLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "recall_proxy_tool_latency_seconds",
		Help:    "Tool call latency in seconds",
		Buckets: prometheus.DefBuckets,
	}, []string{"tool", "server"})

	// ActiveConnections tracks live upstream server connections.
	ActiveConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "recall_proxy_active_connections",
		Help: "Number of active upstream server connections",
	})

	// EngineRequestsTotal counts engine /decide calls.
	EngineRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "recall_proxy_engine_requests_total",
		Help: "Total engine /decide requests by status",
	}, []string{"status"})

	// UpstreamErrorsTotal counts upstream call errors.
	UpstreamErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "recall_proxy_upstream_errors_total",
		Help: "Total upstream errors by server and category",
	}, []string{"server", "error_category"})
)
