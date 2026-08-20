// Package repo — T028 公开查询实现侧测试（纯逻辑；SQL 执行路径走集成测试）
//
// 不与测试专家 integration_test.go 路径重叠；DB 交互用例见 query_integration_test.go。
package repo

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────
// 分页兜底
// ─────────────────────────────────────────────────────────────

func TestNormalizePage_DefaultsAndClamp(t *testing.T) {
	cases := []struct {
		name           string
		in             AlertQueryFilter
		page, pageSize int
	}{
		{"全缺省", AlertQueryFilter{}, 1, 20},
		{"负值兜底", AlertQueryFilter{Page: -1, PageSize: -5}, 1, 20},
		{"上限钳制", AlertQueryFilter{Page: 3, PageSize: 500}, 3, 100},
		{"合法透传", AlertQueryFilter{Page: 2, PageSize: 50}, 2, 50},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.in.NormalizePage()
			assert.Equal(t, tc.page, tc.in.Page)
			assert.Equal(t, tc.pageSize, tc.in.PageSize)
		})
	}
}

func TestOffset(t *testing.T) {
	assert.Equal(t, 0, AlertQueryFilter{Page: 1, PageSize: 20}.Offset())
	assert.Equal(t, 40, AlertQueryFilter{Page: 3, PageSize: 20}.Offset())
	assert.Equal(t, 250, AlertQueryFilter{Page: 6, PageSize: 50}.Offset())
}

// ─────────────────────────────────────────────────────────────
// WHERE 构造（全占位符，无值拼接）
// ─────────────────────────────────────────────────────────────

func TestBuildAlertWhere(t *testing.T) {
	where, args := buildAlertWhere(AlertQueryFilter{})
	assert.Empty(t, where, "无筛选不加 WHERE")
	assert.Empty(t, args)

	where, args = buildAlertWhere(AlertQueryFilter{PatientID: "P001"})
	assert.Equal(t, " WHERE patient_id = $1", where)
	assert.Equal(t, []any{"P001"}, args)

	where, args = buildAlertWhere(AlertQueryFilter{Type: "wear_interrupt"})
	assert.Equal(t, " WHERE type = $1", where)
	assert.Equal(t, []any{"wear_interrupt"}, args)

	where, args = buildAlertWhere(AlertQueryFilter{Status: "pending"})
	assert.Equal(t, " WHERE process_status = $1", where)
	assert.Equal(t, []any{"pending"}, args)

	where, args = buildAlertWhere(AlertQueryFilter{
		PatientID: "P001", Type: "pressure_high", Status: "processed",
	})
	assert.Equal(t, " WHERE patient_id = $1 AND type = $2 AND process_status = $3", where)
	assert.Equal(t, []any{"P001", "pressure_high", "processed"}, args)
}

// ─────────────────────────────────────────────────────────────
// 行扫描（列序 = alertSelectColumns）
// ─────────────────────────────────────────────────────────────

// fakeScanner 按序回填 dest，模拟 pgx.Row/Rows.Scan
type fakeScanner struct {
	vals []any
	err  error
}

func (s *fakeScanner) Scan(dest ...any) error {
	if s.err != nil {
		return s.err
	}
	if len(dest) != len(s.vals) {
		return errors.New("column count mismatch")
	}
	for i, v := range s.vals {
		switch p := dest[i].(type) {
		case *int64:
			*p = v.(int64)
		case *string:
			*p = v.(string)
		case *float64:
			*p = v.(float64)
		case *time.Time:
			*p = v.(time.Time)
		case **time.Time:
			*p = v.(*time.Time)
		case **string:
			*p = v.(*string)
		default:
			return errors.New("unsupported dest type")
		}
	}
	return nil
}

func TestScanAlertRow(t *testing.T) {
	ts := time.Date(2026, 8, 11, 6, 0, 0, 0, time.UTC)
	resolvedAt := ts.Add(time.Hour)
	by := "tech01"
	processedAt := ts.Add(2 * time.Hour)
	note := "ok"

	s := &fakeScanner{vals: []any{
		int64(7), "P001", "DEV01", "sensor_drift",
		"漂移", "P07", 2.8, 3.5,
		ts, "read", "processed", "resolved",
		&resolvedAt, &by, &processedAt, &note,
	}}
	var row AlertRow
	require.NoError(t, scanAlertRow(s, &row))
	assert.Equal(t, int64(7), row.AlertID)
	assert.Equal(t, "P001", row.PatientID)
	assert.Equal(t, "DEV01", row.DeviceID)
	assert.Equal(t, "sensor_drift", row.Type)
	assert.Equal(t, "漂移", row.Detail)
	assert.Equal(t, "P07", row.SensorPoint)
	assert.InDelta(t, 2.8, row.ThresholdValue, 0.001)
	assert.InDelta(t, 3.5, row.ActualValue, 0.001)
	assert.True(t, row.Ts.Equal(ts))
	assert.Equal(t, "read", row.ReadStatus)
	assert.Equal(t, "processed", row.ProcessStatus)
	assert.Equal(t, "resolved", row.ResolvedStatus)
	require.NotNil(t, row.ResolvedAt)
	assert.True(t, row.ResolvedAt.Equal(resolvedAt))
	require.NotNil(t, row.ProcessedBy)
	assert.Equal(t, "tech01", *row.ProcessedBy)
	require.NotNil(t, row.ProcessedAt)
	require.NotNil(t, row.ProcessNote)
	assert.Equal(t, "ok", *row.ProcessNote)
}

func TestScanAlertRow_NullablesNil(t *testing.T) {
	s := &fakeScanner{vals: []any{
		int64(8), "P002", "DEV02", "wear_interrupt",
		"", "", 0.0, 0.0,
		time.Unix(0, 0).UTC(), "unread", "pending", "active",
		(*time.Time)(nil), (*string)(nil), (*time.Time)(nil), (*string)(nil),
	}}
	var row AlertRow
	require.NoError(t, scanAlertRow(s, &row))
	assert.Nil(t, row.ResolvedAt)
	assert.Nil(t, row.ProcessedBy)
	assert.Nil(t, row.ProcessedAt)
	assert.Nil(t, row.ProcessNote)
}

func TestScanAlertRow_Error(t *testing.T) {
	s := &fakeScanner{err: errors.New("scan boom")}
	var row AlertRow
	assert.Error(t, scanAlertRow(s, &row))
}

// alertSelectColumns 列数与 scanAlertRow 目标数一致性（防 schema 漂移；
// 仅计括号外的逗号，COALESCE 内逗号不算列分隔）
func TestSelectColumnsCount(t *testing.T) {
	const want = 16 // scanAlertRow 的 Scan 目标个数
	count, depth := 1, 0
	for _, c := range alertSelectColumns {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				count++
			}
		}
	}
	assert.Equal(t, want, count, "SELECT 列数必须与 scanAlertRow 目标数一致")
}
