//go:build integration
// +build integration

// Package gateway_integration provides integration tests for gateway service.
// Requires Docker to run: make test-integration
package main

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"github.com/bracesync/bracesync/services/testhelper"
	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	testhelper.WithTestContainers(m, func(cfg *testhelper.ContainerConfig) int {
		// Verify PostgreSQL connectivity
		db, err := sql.Open("postgres", cfg.DBURL)
		if err != nil {
			panic("failed to open pg connection: " + err.Error())
		}
		defer db.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			panic("failed to ping pg: " + err.Error())
		}

		return m.Run()
	})
}

func TestPostgresConnectivity(t *testing.T) {
	// 验证 testcontainers PG15 可连接
	dsn := testhelper.GetEnvOrDefault("TEST_DB_URL", "")
	assert.NotEmpty(t, dsn, "TEST_DB_URL should be set by testhelper")

	db, err := sql.Open("postgres", dsn)
	assert.NoError(t, err)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	assert.NoError(t, db.PingContext(ctx))
}

func TestRedisConnectivity(t *testing.T) {
	// 验证 testcontainers Redis7 可连接
	redisURL := testhelper.GetEnvOrDefault("TEST_REDIS_URL", "")
	assert.NotEmpty(t, redisURL, "TEST_REDIS_URL should be set by testhelper")
	// 实际 Redis 客户端连接在后续任务中实现
}
