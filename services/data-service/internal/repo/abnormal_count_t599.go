// T599：daily-wear 异常数读时现算源（与异常报告面同源同窗同分桶）。
package repo

import (
	"context"
	"fmt"
	"time"
)

// AbnormalCountSource 按 CST 日回取「该患者区间内每天的告警条数」的取数契约
// （*RollupRepo 实现）。
// 单独成接口、不并入 DailyWearStatsStore：后者的测试替身遍布各 service 单测，
// 加方法会牵动无关卡片（同 DailyWearDetailSource 的先例）。
// nil = 未注入（仅测试/降级场景）⇒ 调用方回退表内 stored abnormal_count，行为与 T599 之前一致。
type AbnormalCountSource interface {
	// AbnormalCountByCSTDay 按 Asia/Shanghai 切日统计区间内该患者每天的 alerts 行数。
	// from/to 为 UTC 半开窗口 [$1,$2)（与 daily-wear 查询窗口同形）。
	// 返回 map["YYYY-MM-DD"]条数；区间内无告警的日期不出现在 map 里（查得即 0）。
	// 口径与 alert-service 报告面完全一致：同一张 alerts 表、同一 to_char 分桶表达式、
	// 同一半开窗口 —— 这是「患者日报与异常报告页逐日等值」（T599）的同源保证。
	AbnormalCountByCSTDay(ctx context.Context, patientID string, from, to time.Time) (map[string]int, error)
}

// abnormalCountByCSTDaySQL 与 alert-service 报告面（summarizeSQL + buildAlertWhere）同一口径：
//   - 分桶：to_char(ts AT TIME ZONE 'Asia/Shanghai', 'YYYY-MM-DD')（北京日）
//   - 窗口：ts >= $2 AND ts < $3（半开，end = 末日次日 00:00，对齐 T300 报告面 WHERE）
//
// 报告面按 type/status 细分组而这里只按天 —— 消费方要的是逐日总数，多维留待报告面。
const abnormalCountByCSTDaySQL = `
SELECT to_char(a.ts AT TIME ZONE 'Asia/Shanghai', 'YYYY-MM-DD') AS cst_date,
       COUNT(*)::int                                             AS n
  FROM alerts AS a
 WHERE a.patient_id = $1 AND a.ts >= $2 AND a.ts < $3
 GROUP BY 1
 ORDER BY 1`

// AbnormalCountByCSTDay 实现 AbnormalCountSource（RollupRepo 持有 pool，与写入腿同库直连 alerts）
func (r *RollupRepo) AbnormalCountByCSTDay(ctx context.Context, patientID string, from, to time.Time) (map[string]int, error) {
	rows, err := r.pool.Query(ctx, abnormalCountByCSTDaySQL, patientID, from, to)
	if err != nil {
		return nil, fmt.Errorf("abnormal count by cst day: %w", err)
	}
	defer rows.Close()

	out := make(map[string]int)
	for rows.Next() {
		var day string
		var n int
		if err := rows.Scan(&day, &n); err != nil {
			return nil, fmt.Errorf("scan abnormal count row: %w", err)
		}
		out[day] = n
	}
	return out, rows.Err()
}
