// T594 甲案：实时快照「今日累计佩戴时长」与日聚合同源。
//
// 缺陷：旧写侧每枚佩戴帧把设备配置上报间隔（秒，兜 30）当「分钟」HINCRBY，
// 实测放大约 60 倍且无 1440 钳位（staging 当天读到 700+ 小时）。
// 修复：stat:today 只维护佩戴帧数 + 首末帧时刻，读侧走日聚合同一函数
// repo.WearMinutesFromSpan（跨度×实测帧频），与 rollup aggregateDateSQL 同式。
package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// statTodayWearMinutes 是读侧同源换算的纯函数，直接打表。
func TestT594StatTodayWearMinutes_SameSourceAsRollup(t *testing.T) {
	cases := []struct {
		name   string
		stats  map[string]string
		expect int
	}{
		{
			name:   "空统计（未上报/无 key）= 0",
			stats:  map[string]string{},
			expect: 0,
		},
		{
			name:   "单枚佩戴帧不构成时长 = 0（与 WearMinutesFromSpan frames<2 同）",
			stats:  map[string]string{"wear_frames": "1", "wear_first_ts": "1000", "wear_last_ts": "1000"},
			expect: 0,
		},
		{
			name: "两帧跨度 1.5h = 180min（跨度+一个实测帧间隔：5400*2/1/60=180；不看配置间隔）",
			stats: map[string]string{"wear_frames": "2", "wear_first_ts": "1000", "wear_last_ts": "6400"},
			expect: 180,
		},
		{
			name: "600 帧 5h 跨度 ≈301min（跨度+一个约 30s 实测间隔，旧口径会给 600*30=18000min）",
			// WearMinutesFromSpan(600,18000)=round(18000*600/599/60)=round(300.5008)=301
			stats:  map[string]string{"wear_frames": "600", "wear_first_ts": "0", "wear_last_ts": "18000"},
			expect: 301,
		},
		{
			name:   "字段缺失/非法 = 0 不 panic",
			stats:  map[string]string{"wear_frames": "x", "wear_first_ts": "", "wear_last_ts": "100"},
			expect: 0,
		},
		{
			name:   "last<=first 异常窗 = 0",
			stats:  map[string]string{"wear_frames": "5", "wear_first_ts": "2000", "wear_last_ts": "1000"},
			expect: 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.expect, statTodayWearMinutes(c.stats))
		})
	}
}

// 1440 钳位同源：超过物理日的跨度（异常大窗/未来脏帧）夹到 1440。
func TestT594StatTodayWearMinutes_Clamp1440(t *testing.T) {
	// 两帧相隔 25h（90000s）→ 未钳应为 1500min，同源函数夹到 1440
	got := statTodayWearMinutes(map[string]string{
		"wear_frames":   "2",
		"wear_first_ts": "0",
		"wear_last_ts":  "90000",
	})
	assert.Equal(t, 1440, got)
}

// fakeCache 累计侧：乱序帧 first 取 min、last 取 max（补传旧帧/延迟帧都不破坏跨度）。
func TestT594FakeCache_FrameSpanOutOfOrder(t *testing.T) {
	env := newTestEnv()
	base := fixedNow.Add(-2 * time.Hour)

	// 先发中间帧，再发更早帧（补传），最后发更晚帧
	uploads := []time.Time{
		base.Add(20 * time.Minute),
		base.Add(-10 * time.Minute), // 更早，应成为 first
		base.Add(40 * time.Minute),  // 更晚，应成为 last
	}
	for _, ts := range uploads {
		_, appErr := env.svc.UploadSingle(context.Background(), testDevice, singleReq(ts, pts(10, 20)))
		assert.Nil(t, appErr)
	}
	st := env.cache.stat[testPatient]
	assert.Equal(t, 3, st.frames)
	assert.True(t, st.first.Equal(base.Add(-10*time.Minute)), "first 必须回退到最早帧")
	assert.True(t, st.last.Equal(base.Add(40*time.Minute)), "last 必须推进到最晚帧")

	// 读侧：3 帧跨度 50min（3000s）→ WearMinutesFromSpan(3,3000)=round(3000*3/2/60)=75min
	stats, err := env.cache.GetStatToday(context.Background(), testPatient)
	assert.Nil(t, err)
	assert.Equal(t, 75, statTodayWearMinutes(stats))
}
