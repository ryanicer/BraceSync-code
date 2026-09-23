// Package service T366：seed 示例聚合行的「不可复算」守卫
//
// seed.sql 往 daily_wear_stats 里手写了一批演示行（喂 Dashboard 佩戴趋势），
// 而同一文件的 pressure_records 明细只有 6 帧 —— 这些示例行声明的帧数在数学上
// 不可能由现存明细算出来。读侧的三值 provenance 正是靠这一点把它们判成 unsupported。
//
// 本用例把该事实钉成回归守卫：
//   - seed 若再偷偷加/改聚合行而不核明细 → 行数或帧数断言变红，逼作者重新看一眼；
//   - seed 若学会给示例行盖聚合印章 → 直接判红（示例行永远不许自称可信聚合）。
package service

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	// seed 示例聚合行：(patient_id, stat_date, wear_minutes, avg, max, max_point, frame_count, abnormal_count)
	reSeedWearRow = regexp.MustCompile(`'([A-Z]\d+)',\s*'(\d{4}-\d{2}-\d{2})',\s*(\d+),\s*[\d.]+,\s*[\d.]+,\s*'P\d{2}',\s*(\d+),\s*(\d+)`)
	// seed 明细帧：(device_id, patient_id, ts)，ts 是带 +08 偏移的字面量 ⇒ 日期部分即 CST 日
	reSeedFrame = regexp.MustCompile(`'PRS-[\w-]+',\s*'([A-Z]\d+)',\s*'(\d{4}-\d{2}-\d{2})\s`)
)

// sqlStatement 取 marker 起、到下一个语句结束符为止的原文块
func sqlStatement(t *testing.T, text, marker string) string {
	t.Helper()
	start := -1
	for i := 0; i+len(marker) <= len(text); i++ {
		if text[i:i+len(marker)] == marker {
			start = i
			break
		}
	}
	require.GreaterOrEqual(t, start, 0, "seed.sql 里找不到 %s 块", marker)
	end := start
	for end < len(text) && text[end] != ';' {
		end++
	}
	require.Less(t, end, len(text), "%s 块没有结尾分号", marker)
	return text[start:end]
}

// seedFile 定位 scripts/db/seed/seed.sql 并读出（相对本文件 4 级上，同 IT 的 migrationsDir 口径）
func seedFile(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "scripts", "db", "seed", "seed.sql")
	b, err := os.ReadFile(path)
	require.NoError(t, err, "读 seed.sql 失败：%s", path)
	return string(b)
}

func TestT366SeedDemoWearRowsAreNotRecomputable(t *testing.T) {
	text := seedFile(t)

	wearBlock := sqlStatement(t, text, "INSERT INTO daily_wear_stats")
	frameBlock := sqlStatement(t, text, "INSERT INTO pressure_records")

	// 明细：按 (患者, CST 日) 计数
	detail := map[string]int{}
	for _, m := range reSeedFrame.FindAllStringSubmatch(frameBlock, -1) {
		detail[m[1]+" "+m[2]]++
	}
	totalFrames := 0
	for _, n := range detail {
		totalFrames += n
	}
	require.Greater(t, totalFrames, 0, "seed 明细帧解析失败（正则没匹配到任何行）")

	wearRows := reSeedWearRow.FindAllStringSubmatch(wearBlock, -1)
	require.NotEmpty(t, wearRows, "seed 示例聚合行解析失败（正则没匹配到任何行）")
	// 与 seed.sql:144-154 同源的行数镜像。改 seed 就要同时改这里 —— 这是刻意留的摩擦：
	// 演示数据与聚合口径互相牵制，加行的人必须看一眼「这行能不能被明细佐证」。
	assert.Len(t, wearRows, 9, "seed.sql 的示例聚合行数变了，请同步本判据与 docs/api/api-contracts.ts 的 provenance 说明")

	unverifiable := 0
	for _, r := range wearRows {
		patient, date := r[1], r[2]
		declared, err := strconv.Atoi(r[4])
		require.NoError(t, err)
		actual := detail[patient+" "+date]
		if declared != actual {
			unverifiable++
		}
		assert.NotEqual(t, declared, actual,
			"seed 聚合行 %s/%s 的 frame_count 与明细帧数相等 ⇒ 这一行不再是纯演示数据，"+
				"要么改走聚合任务写入，要么在本判据里说明它为何例外", patient, date)
	}
	assert.Equal(t, len(wearRows), unverifiable,
		"每一条 seed 示例聚合行都必须「无明细佐证」，否则读侧 unsupported 判档失去依据")

	// 🔴 示例行永远不许自称有聚合印章：印章列只由 RollupService 的 UPSERT 写
	assert.NotContains(t, wearBlock, "aggregated_at", "seed 不得给示例行盖聚合印章")
	assert.NotContains(t, wearBlock, "wearing_threshold_n", "seed 不得给示例行盖聚合印章")
}
