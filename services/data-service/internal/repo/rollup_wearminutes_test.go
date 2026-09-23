// Package repo T352：日聚合佩戴分钟折算口径单测（跨度 + 一个实测帧间隔）
package repo

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

func TestWearMinutesFromSpan(t *testing.T) {
	cases := []struct {
		name          string
		wearingFrames int
		spanSeconds   float64
		want          int
	}{
		{"无佩戴帧", 0, 0, 0},
		{"单帧无跨度不臆造", 1, 0, 0},
		{"两帧间隔一分钟", 2, 60, 2},              // 跨度 1min + 一个间隔 = 2min
		{"三帧间隔三十秒", 3, 60, 2},              // 跨度 60s → 90s
		{"五帧小时间隔", 5, 4 * 3600, 300},       // 跨度 4h → 5h
		{"十帧五百零四秒", 10, 0.14 * 3600, 9},    // 560s
		{"一千四百五十六帧", 1456, 1455 * 31, 752}, // staging 实测 31s 节奏
		{"跨度近满日仍封顶", 2, 23.9 * 3600, 1440}, // 异常窗口防御
		{"零跨度", 10, 0, 0},
		{"负跨度", 10, -5, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := wearMinutesFromSpan(c.wearingFrames, c.spanSeconds)
			assert.Equal(t, c.want, got)
			assert.LessOrEqual(t, got, model.MaxWearMinutesPerDay)
		})
	}
}

// 折算结果与「帧数 × 实测间隔」在 ±1 帧内自洽（T352 验收判据：小时数与帧时间跨度自洽）
func TestWearMinutesConsistentWithFrameCountTimesMeasuredInterval(t *testing.T) {
	const intervalSeconds = 31 // staging 实测上报节奏
	for _, frames := range []int{2, 10, 248, 556, 1456} {
		span := float64((frames - 1) * intervalSeconds)
		got := wearMinutesFromSpan(frames, span)
		want := frames * intervalSeconds / 60
		assert.LessOrEqual(t, absInt(got-want), 1,
			fmt.Sprintf("frames=%d span=%.0fs", frames, span))
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
