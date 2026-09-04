// Package testhelper 提供 BraceSync 集成测试共享工具
// 对齐：docs/ §6.2 · docs/ §1
//
// 使用方式（各服务 *_integration_test.go）：
//
//	func TestMain(m *testing.M) {
//	    testhelper.WithTestContainers(m, func(cfg *testhelper.ContainerConfig) int {
//	        // 在此做迁移/种子等初始化
//	        return m.Run()
//	    })
//	}
package testhelper

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"
)

// ContainerConfig 测试容器连接信息
type ContainerConfig struct {
	DBURL    string // PostgreSQL DSN
	RedisURL string // Redis 地址
}

// WithTestContainers 启动 PG15 + Redis7 测试容器，调用 run 执行测试，最后清理容器
func WithTestContainers(m *testing.M, run func(cfg *ContainerConfig) int) {
	ctx := context.Background()

	// 启动 PostgreSQL 15 容器
	pgContainer, err := postgres.Run(ctx,
		"postgres:15-alpine",
		postgres.WithDatabase("bracesync_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "testhelper: failed to start postgres container: %v\n", err)
		os.Exit(1)
	}

	// 启动 Redis 7 容器
	redisContainer, err := redis.Run(ctx,
		"redis:7-alpine",
		testcontainers.WithWaitStrategy(
			wait.ForLog("Ready to accept connections").
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "testhelper: failed to start redis container: %v\n", err)
		os.Exit(1)
	}

	// 获取连接字符串
	pgDSN, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintf(os.Stderr, "testhelper: failed to get pg connection string: %v\n", err)
		os.Exit(1)
	}

	redisURL, err := redisContainer.ConnectionString(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "testhelper: failed to get redis connection string: %v\n", err)
		os.Exit(1)
	}

	cfg := &ContainerConfig{
		DBURL:    pgDSN,
		RedisURL: redisURL,
	}

	// 注入环境变量，供业务代码使用
	_ = os.Setenv("TEST_DB_URL", cfg.DBURL)
	_ = os.Setenv("TEST_REDIS_URL", cfg.RedisURL)

	// 执行测试
	exitCode := run(cfg)

	// 清理容器
	if err := pgContainer.Terminate(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "testhelper: failed to terminate pg container: %v\n", err)
	}
	if err := redisContainer.Terminate(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "testhelper: failed to terminate redis container: %v\n", err)
	}

	os.Exit(exitCode)
}

func GetEnvOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// WithTransactionRollback 在事务上下文中执行 run() 函数并在结束后自动回滚（用于测试隔离）
// 适用于每测试用例独立数据库状态验证，避免跨用例污染
// usage: 
//   err := WithTransactionRollback(ctx, store, func() error {
//       // ... 测试操作 ...
//       return nil
//   })
// TODO: Winner 实现后需与 repo.Store 接口集成（目前 standalone helper）
