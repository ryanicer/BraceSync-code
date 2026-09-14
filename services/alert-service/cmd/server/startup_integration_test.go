//go:build integration
// +build integration

// T202 启动时序回归冒烟：真实 PG + Redis 下拉起 alert-service 可执行文件。
//
// 覆盖的是单测/契约测试都摸不到的一条路径：main() 里 scheduler.Start 会同步补跑一轮，
// 该轮回调向 consumer 注入热更新佩戴阈值（T173 引入）。装配顺序一旦把消费者构造挪到
// 调度器之后，进程启动即 nil 指针 panic，HTTP 端口永远起不来。
//
// 运行：go test -tags=integration ./services/alert-service/...（CI go-integration job）
package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/testhelper"
)

var (
	itDBURL    string
	itRedisURL string
)

func TestMain(m *testing.M) {
	testhelper.WithTestContainers(m, func(cfg *testhelper.ContainerConfig) int {
		ctx := context.Background()
		pool, err := pgxpool.New(ctx, cfg.DBURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "startup-it: pgxpool: %v\n", err)
			return 1
		}
		defer pool.Close()
		if err := applyMigrations(ctx, pool); err != nil {
			fmt.Fprintf(os.Stderr, "startup-it: %v\n", err)
			return 1
		}
		itDBURL = cfg.DBURL
		itRedisURL = cfg.RedisURL
		return m.Run()
	})
}

// applyMigrations 顺序执行 scripts/db/migrations 全部 *.up.sql（与 repo 集成测试同一事实源）。
// sys_configs 必须存在：缺失时 cfgMgr.Refresh 直接报错返回，走不到阈值注入分支，测试会假绿。
func applyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "scripts", "db", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	for _, name := range files {
		sqlBytes, readErr := os.ReadFile(filepath.Join(dir, name))
		if readErr != nil {
			return fmt.Errorf("read migration %s: %w", name, readErr)
		}
		if _, execErr := pool.Exec(ctx, string(sqlBytes)); execErr != nil {
			return fmt.Errorf("apply %s: %w", name, execErr)
		}
	}
	return nil
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

// safeBuf 并发安全的子进程日志缓冲（exec 的拷贝协程与测试断言 goroutine 同时访问）。
type safeBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestStartupSurvivesImmediateScan 断言进程熬过启动补跑并保持存活，且 /healthz 返回 200。
func TestStartupSurvivesImmediateScan(t *testing.T) {
	port := freePort(t)

	_, thisFile, _, _ := runtime.Caller(0)
	pkgDir := filepath.Dir(thisFile)

	bin := filepath.Join(t.TempDir(), "alert-service")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = pkgDir
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build alert-service: %v\n%s", err, out)
	}

	srv := exec.Command(bin)
	srv.Dir = pkgDir
	srv.Env = append(os.Environ(),
		"DB_URL="+itDBURL,
		"REDIS_URL="+itRedisURL,
		"PORT="+fmt.Sprint(port),
		"PENDING_POLL_MS=200",
	)
	var logs safeBuf
	srv.Stdout = &logs
	srv.Stderr = &logs
	require.NoError(t, srv.Start(), "启动 alert-service 进程")

	var waitErr error
	waitDone := make(chan struct{})
	go func() { waitErr = srv.Wait(); close(waitDone) }()

	t.Cleanup(func() {
		_ = srv.Process.Kill()
		select {
		case <-waitDone:
		case <-time.After(10 * time.Second):
			t.Errorf("alert-service 子进程未在 10s 内退出")
		}
	})

	crashed := func() bool {
		select {
		case <-waitDone:
			return true
		default:
			return false
		}
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
	deadline := time.Now().Add(20 * time.Second)
	var code int
	for time.Now().Before(deadline) {
		if crashed() {
			t.Fatalf("alert-service 启动即退出（%v），从未提供 /healthz\n输出:\n%s", waitErr, logs.String())
		}
		resp, err := http.Get(url) //nolint:noctx // 冒烟探针
		if err == nil {
			code = resp.StatusCode
			_ = resp.Body.Close()
			if code == http.StatusOK {
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}

	require.Equal(t, http.StatusOK, code, "/healthz 应返回 200，输出:\n%s", logs.String())

	// 断言走到过阈值注入分支：sys_configs 读失败时该分支被跳过，panic 也不会发生，
	// 只断言 /healthz 会在 seed/migration 变化后退化成假绿。
	require.Contains(t, logs.String(), "wearing threshold injected into pending consumer",
		"启动补跑轮未执行佩戴阈值注入，测试未覆盖缺陷路径，输出:\n%s", logs.String())

	// 补跑轮之后的持续存活：崩溃（panic 后进程退出）在这里被抓
	select {
	case <-waitDone:
		t.Fatalf("alert-service 在 /healthz 通过后退出（%v），输出:\n%s", waitErr, logs.String())
	case <-time.After(3 * time.Second):
	}
}
