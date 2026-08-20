//go:build integration
// +build integration

// Package repo 集成测试（T030）：健康报告查询 SQL（真实 PG15，testcontainers）
package repo

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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
	"github.com/bracesync/bracesync/services/testhelper"
)

var rqPool *pgxpool.Pool
var dashPool *pgxpool.Pool // Dashboard integration pool

func TestMain(m *testing.M) {
	testhelper.WithTestContainers(m, func(cfg *testhelper.ContainerConfig) int {
		return runRepoAndDashboardIT(m, cfg.DBURL)
	})
}

func runRepoAndDashboardIT(m *testing.M, dbURL string) int {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "it: pgxpool: %v\n", err)
		return 1
	}
	rqPool = pool
	dashPool = pool // 两个测试用同一个 DB pool
	defer pool.Close()

	applyReportsMigrations(ctx)
	seedReportsData(ctx)
	// Dashboard 集成测试复用同一 PG 容器：执行 dashboard seed
	seedDashboardData(ctx)
	return m.Run()
}

// migrationsDir 定位 scripts/db/migrations（相对本文件 4 级上）
func migrationsDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "scripts", "db", "migrations")
}

func applyReportsMigrations(ctx context.Context) {
	entries, err := os.ReadDir(migrationsDir())
	if err != nil {
		panic("it: read migrations dir: " + err.Error())
	}
	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	for _, name := range files {
		sqlBytes, readErr := os.ReadFile(filepath.Join(migrationsDir(), name))
		if readErr != nil {
			panic("it: read migration: " + readErr.Error())
		}
		if _, execErr := rqPool.Exec(ctx, string(sqlBytes)); execErr != nil {
			panic(fmt.Sprintf("it: apply %s: %v", name, execErr))
		}
	}
}

const rqPatient = "P-RPT-IT-001"

func seedReportsData(ctx context.Context) {
	stmts := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO patients (patient_id, name, phone_enc, phone_hash, status)
		 VALUES ($1, '报告患者', '\x00'::bytea, 'rp01' || repeat('0', 60), 'active')
		 ON CONFLICT (patient_id) DO NOTHING`, []any{rqPatient}},
		{`INSERT INTO health_reports (patient_id, report_type, period_start, period_end,
		    wear_compliance_rate, avg_pressure, trend_judgment, suggestion)
		 VALUES ($1, 'weekly', '2026-07-20', '2026-07-26', 85.00, 22.4, 'flat', '第一周'),
		        ($1, 'weekly', '2026-07-27', '2026-08-02', 90.00, 21.0, 'up', '第二周'),
		        ($1, 'monthly', '2026-07-01', '2026-07-31', NULL, NULL, NULL, NULL)
		 ON CONFLICT (patient_id, report_type, period_start) DO NOTHING`, []any{rqPatient}},
	}
	for _, s := range stmts {
		if _, err := rqPool.Exec(ctx, s.sql, s.args...); err != nil {
			panic("it: seed: " + err.Error())
		}
	}
}

func TestITListReportsOrderAndCoalesce(t *testing.T) {
	repo := NewReportRepo(rqPool)
	list, err := repo.ListReports(context.Background(), rqPatient)
	require.NoError(t, err)
	require.Len(t, list, 3)

	// period_start 倒序：07-27 周 > 07-20 周 > 07-01 月
	assert.Equal(t, "2026-07-27", list[0].PeriodStart.Format("2006-01-02"))
	assert.Equal(t, "第二周", list[0].Suggestion)

	// NULL 字段 COALESCE 兜底（monthly 行）
	var monthly *model.HealthReport
	for i := range list {
		if list[i].ReportType == "monthly" {
			monthly = &list[i]
		}
	}
	require.NotNil(t, monthly)
	assert.Equal(t, 0.0, monthly.WearComplianceRate)
	assert.Equal(t, "flat", monthly.TrendJudgment)
	assert.Equal(t, "", monthly.Suggestion)

	// 无报告患者 → 空列表
	empty, err := repo.ListReports(context.Background(), "P-NO-RPT")
	require.NoError(t, err)
	assert.Empty(t, empty)
}
