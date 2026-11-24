// Package redis provides a shared Redis client for multi-pod state sync.
// When Redis is not configured, the gateway runs in standalone mode (SQLite + memory).
package redis

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// Client wraps a Redis connection for the gateway.
type Client struct {
	rdb *redis.Client
}

// Config holds Redis connection parameters.
type Config struct {
	URL      string
	PoolSize int
	Password string
	TLS      bool
}

// NewClient creates a Redis client. Returns nil if URL is empty.
func NewClient(cfg Config) (*Client, error) {
	if cfg.URL == "" {
		return nil, nil
	}
	opts, err := redis.ParseURL(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse redis URL: %w", err)
	}
	if cfg.Password != "" {
		opts.Password = cfg.Password
	}
	if cfg.PoolSize > 0 {
		opts.PoolSize = cfg.PoolSize
	} else {
		opts.PoolSize = 10
	}

	rdb := redis.NewClient(opts)

	// Health check with 5-second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		rdb.Close()
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	slog.Info("redis connected", "url", cfg.URL, "pool_size", opts.PoolSize)
	return &Client{rdb: rdb}, nil
}

// Unwrap returns the underlying go-redis client for direct use by stores.
func (c *Client) Unwrap() *redis.Client { return c.rdb }

// Close shuts down the Redis connection pool.
func (c *Client) Close() error {
	if c != nil && c.rdb != nil {
		return c.rdb.Close()
	}
	return nil
}

// IsConnected checks if Redis is reachable.
func (c *Client) IsConnected() bool {
	if c == nil || c.rdb == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return c.rdb.Ping(ctx).Err() == nil
}
