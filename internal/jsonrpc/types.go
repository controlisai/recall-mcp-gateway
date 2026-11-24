package jsonrpc

import "encoding/json"

// Message represents a JSON-RPC 2.0 message (request, response, or notification).
type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// ToolCallParams represents the params for a tools/call request.
type ToolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
	Meta      map[string]any `json:"_meta,omitempty"`
}

// InitializeParams represents the params from an MCP initialize request.
type InitializeParams struct {
	ProtocolVersion string         `json:"protocolVersion,omitempty"`
	ClientInfo      ClientInfo     `json:"clientInfo,omitempty"`
	Meta            map[string]any `json:"_meta,omitempty"`
}

// ClientInfo carries client identification from the MCP initialize handshake.
type ClientInfo struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

// IsToolCall returns true if this message is a tools/call request.
func (m *Message) IsToolCall() bool {
	return m.Method == "tools/call"
}

// IsInitialize returns true if this message is an initialize request.
func (m *Message) IsInitialize() bool {
	return m.Method == "initialize"
}

// IsRequest returns true if the message has a method (is a request or notification).
func (m *Message) IsRequest() bool {
	return m.Method != ""
}
