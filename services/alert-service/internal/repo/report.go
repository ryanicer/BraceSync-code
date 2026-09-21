// Package repo — T300 患者异常报告汇总/导出数据访问（合同 §二 患者管理「患者异常报告汇总与导出」）。
//
// 支撑 GET /api/v1/admin/abnormal-reports（按患者 + 日期范围汇总）与
// GET /api/v1/admin/abnormal-reports/export（同口径 CSV 明细）。
//
// 索引口径（架构 §4.4）：patient_id → idx_alerts_patient_ts (patient_id, ts DESC)，
// 故「患者 + ts 半开区间」是本文件两条查询的推荐形状；分组汇总走同一 WHERE。
package repo

import (
	"context"
	"strconv"
)

// MaxExportRows 单次导出的明细行数上限。
// 超出部分不返回（handler 据 truncated 在 CSV 尾部与响应头告知），
// 避免后台一次导出把整表扫进内存。
const MaxExportRows = 5000

// AlertSummaryRow 汇总单元（北京日 × 告警类型 × 处理状态 的一格计数）。
// 一次 GROUP BY 即可派生「按天」「按类型」「按状态」三个视角，
// handler 侧只做加法，不再回表。
type AlertSummaryRow struct {
	Date          string // YYYY-MM-DD（Asia/Shanghai 口径，与 ts 的 AT TIME ZONE 一致）
	Type          string
	ProcessStatus string
	Count         int64
}

// summarizeSQL 分组汇总（列序 = scanAlertSummary）。
// GROUP BY 用输出列名 day 是 PG 合法写法；ts 的时区换算只发生在展示层，
// 不参与筛选（筛选走 StartTs/EndTs 的 timestamptz 半开区间，可用索引）。
const summarizeSQL = `
	SELECT to_char(a.ts AT TIME ZONE 'Asia/Shanghai', 'YYYY-MM-DD') AS day,
	       a.type, a.process_status, COUNT(*) AS n
	  FROM alerts AS a`

// SummarizeAlerts 按患者 + 日期范围汇总告警（无分页；分组行数 ≤ 天数×类型×3）。
func (r *PGAlertRepo) SummarizeAlerts(ctx context.Context, f AlertQueryFilter) ([]AlertSummaryRow, error) {
	where, args := buildAlertWhere(f)
	rows, err := r.pool.Query(ctx, summarizeSQL+where+
		` GROUP BY day, a.type, a.process_status ORDER BY day DESC, a.type, a.process_status`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]AlertSummaryRow, 0, 32)
	for rows.Next() {
		var s AlertSummaryRow
		if err := rows.Scan(&s.Date, &s.Type, &s.ProcessStatus, &s.Count); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListAlertsForExport 导出用明细查询：与 ListAlerts 同一投影与排序，但不分页，
// 改为「最多取 MaxExportRows 条」。limit 由调用方给（handler 传常量），
// 返回值 truncated = true 表示范围内还有数据未取（SQL 多取一条探测）。
func (r *PGAlertRepo) ListAlertsForExport(ctx context.Context, f AlertQueryFilter, limit int) ([]AlertRow, bool, error) {
	if limit < 1 {
		limit = MaxExportRows
	}
	where, args := buildAlertWhere(f)
	probeArgs := append(append([]any{}, args...), limit+1)
	rows, err := r.pool.Query(ctx,
		`SELECT `+alertSelectColumns+` FROM alerts AS a LEFT JOIN patients AS p ON a.patient_id = p.patient_id`+where+
			` ORDER BY a.ts DESC, a.alert_id DESC LIMIT $`+strconv.Itoa(len(args)+1), probeArgs...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	out := make([]AlertRow, 0, limit)
	truncated := false
	for rows.Next() {
		var row AlertRow
		if err := scanAlertRow(rows, &row); err != nil {
			return nil, false, err
		}
		if len(out) == limit {
			truncated = true // 探测行：只计数一次，不继续物化
			break
		}
		out = append(out, row)
	}
	return out, truncated, rows.Err()
}
