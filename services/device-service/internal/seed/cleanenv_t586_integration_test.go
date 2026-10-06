//go:build integration
// +build integration

// T586 判据 4（本卡核心）：干净环境跑一次真实装载，断言不再长出不可解设备行。
//
// 这里不复用其它集成测试的种子：本包自己起 PG15 容器、按序跑 migrations、
// 再把仓内 scripts/db/seed/seed.sql 原文交给 psql 协议执行（与 Makefile 的
// seed 目标同一份字节），然后在库侧量字节长度。
package seed

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bracesync/bracesync/services/testhelper"
)

var cleanPool *pgxpool.Pool

func TestMain(m *testing.M) {
	testhelper.WithTestContainers(m, func(cfg *testhelper.ContainerConfig) int {
		return runCleanEnv(m, cfg.DBURL)
	})
}

func repoRoot() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		panic("runtime.Caller 定位不到本文件")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
}

func runCleanEnv(m *testing.M, dbURL string) int {
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "t586: pgxpool: %v\n", err)
		return 1
	}
	cleanPool = pool
	defer pool.Close()

	dir := filepath.Join(repoRoot(), "scripts", "db", "migrations")
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		fmt.Fprintf(os.Stderr, "t586: read migrations dir: %v\n", readErr)
		return 1
	}
	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	for _, name := range files {
		body, bodyErr := os.ReadFile(filepath.Join(dir, name))
		if bodyErr != nil {
			fmt.Fprintf(os.Stderr, "t586: read %s: %v\n", name, bodyErr)
			return 1
		}
		if _, execErr := cleanPool.Exec(ctx, string(body)); execErr != nil {
			fmt.Fprintf(os.Stderr, "t586: apply %s: %v\n", name, execErr)
			return 1
		}
	}

	seedPath := filepath.Join(repoRoot(), "scripts", "db", "seed", "seed.sql")
	seedBody, seedErr := os.ReadFile(seedPath)
	if seedErr != nil {
		fmt.Fprintf(os.Stderr, "t586: read seed.sql: %v\n", seedErr)
		return 1
	}
	if _, loadErr := cleanPool.Exec(ctx, string(seedBody)); loadErr != nil {
		fmt.Fprintf(os.Stderr, "t586: load seed.sql: %v\n", loadErr)
		return 1
	}
	return m.Run()
}

// countQuery 跑一条回单值的 SELECT。
func countQuery(ctx context.Context, t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := cleanPool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", sql, err)
	}
	return n
}

// unsolvableDeviceRows 是库侧那把尺：密钥列非空但短于 nonce 的设备行。
func unsolvableDeviceRows(ctx context.Context, t *testing.T) int {
	t.Helper()
	return countQuery(ctx, t,
		"SELECT count(*) FROM devices WHERE octet_length(device_secret_enc) > 0 AND octet_length(device_secret_enc) < $1",
		minCipherLen)
}

func TestT586_CleanEnvLoadProducesNoUnsolvableDevice(t *testing.T) {
	ctx := context.Background()

	rows := countQuery(ctx, t, "SELECT count(*) FROM devices")
	bad := unsolvableDeviceRows(ctx, t)
	t.Logf("干净库装载后：设备行 %d · 不可解设备行 %d", rows, bad)

	if rows != len(t586DeviceIDs) {
		t.Fatalf("装载后设备行 %d，期望 %d", rows, len(t586DeviceIDs))
	}
	if bad != 0 {
		t.Fatalf("新生环境装载仍长出 %d 行不可解设备（判据 4 未过）", bad)
	}
}

func TestT586_CleanEnvDeviceSecretsDecrypt(t *testing.T) {
	ctx := context.Background()
	gcm := gcmFor(t, fixtureKeyHex())

	rows, err := cleanPool.Query(ctx, "SELECT device_id, device_secret_enc FROM devices ORDER BY device_id")
	if err != nil {
		t.Fatalf("query devices: %v", err)
	}
	defer rows.Close()

	checked := 0
	for rows.Next() {
		var id string
		var enc []byte
		if scanErr := rows.Scan(&id, &enc); scanErr != nil {
			t.Fatalf("scan %s: %v", id, scanErr)
		}
		plain, decErr := decryptBytes(gcm, enc)
		if decErr != nil {
			t.Fatalf("%s 在库里的字节用夹具钥解不出来：%v", id, decErr)
		}
		want := fixturePlainHex(id)
		if plain != want {
			t.Fatalf("%s 明文不符：库里解得 %d 位，文档化明文 %d 位", id, len(plain), len(want))
		}
		checked++
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		t.Fatalf("rows: %v", rowsErr)
	}
	if checked != len(t586DeviceIDs) {
		t.Fatalf("逐行验签 %d 台，期望 %d 台", checked, len(t586DeviceIDs))
	}
	t.Logf("库里 %d 台设备的 device_secret_enc 全部解得出文档化明文", checked)
}

func TestT586_CleanEnvPhoneColumnsHoldNoCiphertext(t *testing.T) {
	ctx := context.Background()
	checks := []string{
		"SELECT count(*) FROM doctors WHERE octet_length(phone_enc) > 0",
		"SELECT count(*) FROM technicians WHERE octet_length(phone_enc) > 0",
		"SELECT count(*) FROM patients WHERE octet_length(phone_enc) > 0",
	}
	for _, sql := range checks {
		n := countQuery(ctx, t, sql)
		t.Logf("%s：带密文的行 %d", strings.Fields(sql)[3], n)
		if n != 0 {
			t.Fatalf("%s 里有 %d 行 phone_enc 带密文（占位密钥回归）", strings.Fields(sql)[3], n)
		}
	}
}

// TestT586_CleanEnvRulerHasTeethOnInjectedPlaceholder 注牙：往同一张干净表插一行
// 1 字节占位密钥，同一把库侧尺必须咬住；删掉后必须回到 0。
func TestT586_CleanEnvRulerHasTeethOnInjectedPlaceholder(t *testing.T) {
	ctx := context.Background()
	const injectID = "PRS-T586-INJECT-001"

	sql := "INSERT INTO devices (device_id, device_secret_enc, status) VALUES ($1, '\\x00'::bytea, 'unbound')"
	if _, err := cleanPool.Exec(ctx, sql, injectID); err != nil {
		t.Fatalf("注牙插入失败：%v", err)
	}

	before := unsolvableDeviceRows(ctx, t)
	t.Logf("注牙后：不可解设备行 %d", before)
	if before != 1 {
		t.Fatalf("守卫没牙：注牙一行 1 字节占位后应读到 1，实得 %d", before)
	}

	if _, err := cleanPool.Exec(ctx, "DELETE FROM devices WHERE device_id = $1", injectID); err != nil {
		t.Fatalf("注牙行删除失败：%v", err)
	}
	after := unsolvableDeviceRows(ctx, t)
	t.Logf("拔掉注牙后：不可解设备行 %d", after)
	if after != 0 {
		t.Fatalf("注牙行没拔干净，实得 %d", after)
	}
}
