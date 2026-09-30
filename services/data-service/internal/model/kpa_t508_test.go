// T508 展示层 N 到 kPa 的换算（PRD §7A.2.1 三 + 稿面图 kpaOf 现文）。
//
// 本文件守四件事：
//  1. PRD 参考换算表逐格（默认面积 0.64 cm² 下 0 / 1 / 2 / 4 / 6 N）——
//     4 N 那格是 half up 的分界（62.5），取整法一错就静默差 1；
//  2. 面积非法（缺失 / 0 / 负 / NaN / Inf）一律 nil，前端显示 --，不退化成 0；
//     默认值 0.64 是后台配置项的默认值，读侧不拿它补位（§五.3 的硬约束）；
//  3. Go 侧的两个量纲陷阱：math.Max(0, NaN) 返回 0（会把 NaN 洗成合法零值）、
//     极小面积把结果推出 int32（溢出成负数是假读数）；
//  4. BuildHeatmap 的 kPa 只是数值文本：IsMax 与色阶分档继续按 N（§三 末条）。
package model

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func t508Area(v float64) *float64 { return &v }

// t508Kpa 取换算结果；期望非 nil 的用例走这个，nil 的用例直接断言 KpaFromN 返回 nil。
func t508Kpa(t *testing.T, n float64, area *float64) int {
	t.Helper()
	got := KpaFromN(n, area)
	require.NotNil(t, got, "n=%v area=%v 期望可换算，实际判为不可换算", n, area)
	return *got
}

// PRD §7A.2.1 三「默认面积下的参考换算表」逐格。面积由参数给，不许在实现里写死 0.64。
func TestT508_KpaFromN_PrdReferenceTable(t *testing.T) {
	area := t508Area(0.64)

	cases := []struct {
		n    float64
		want int
		why  string
	}{
		{0, 0, "0 N 仍 0（含归零后的负值）"},
		{1, 16, "15.625 半值以上，进 16；对应 PM 那句「1 N 约 15.6 kPa」的显示值"},
		{2, 31, "低压/正常分界线 31.25，判档仍按 N"},
		{4, 63, "half up 边界：62.5 必须给 63 而不是 62"},
		{6, 94, "色阶上界 93.75，仍由 heatmapMaxN 下发"},
	}
	for _, c := range cases {
		t.Run(c.why, func(t *testing.T) {
			assert.Equal(t, c.want, t508Kpa(t, c.n, area), "n=%v", c.n)
		})
	}
}

// 三步序 ① 归零：负值是零点漂移，不折算成负压力（T322/T233 既有口径）。
func TestT508_KpaFromN_NegativeZeroesFirst(t *testing.T) {
	area := t508Area(0.64)
	assert.Equal(t, 0, t508Kpa(t, -0.2, area), "负值先归零，不得出现 -0.2 kPa 这类未裁过的负压强")
	assert.Equal(t, 0, t508Kpa(t, math.SmallestNonzeroFloat64*-1, area))
	assert.Equal(t, 0, t508Kpa(t, -1, area), "负值先归零：-1 N 显示 0 kPa，而不是 -16")
	assert.Equal(t, 16, t508Kpa(t, 1, area), "归零只作用于负数本身，同号正数那格不受牵连")
}

// PRD 表 0.014 N 那格显示值写 0，稿面图 kpaOf 现文是 Math.max(1, Math.round(k))；
// 本实现按稿面（设计稿口径高于 PRD 表），并已把「真非零最小 1 kPa」作为待裁项登记在卡上。
func TestT508_KpaFromN_NonZeroClampsToOne(t *testing.T) {
	area := t508Area(0.64)
	assert.Equal(t, 1, t508Kpa(t, 0.014, area), "0.21875 取整为 0，但真非零压力不得读成 0 kPa")
	assert.Equal(t, 1, t508Kpa(t, 0.001, area))
	assert.Equal(t, 0, t508Kpa(t, 0, area), "钳位只针对真非零，0 N 仍是 0")
}

// §五.3 fail-closed：面积缺失 / 非正数 / 非有限 → 不可换算（nil），前端显示 --。
// 每格都要走 nil 而不是 0：0 kPa 是「读到零压力」，与「配置缺失」是两回事。
func TestT508_KpaFromN_InvalidAreaFailsClosed(t *testing.T) {
	for name, area := range map[string]*float64{
		"未配置": nil,
		"零":   t508Area(0),
		"负":   t508Area(-0.64),
		"NaN": t508Area(math.NaN()),
		"正无穷": t508Area(math.Inf(1)),
		"负无穷": t508Area(math.Inf(-1)),
		"负零":  t508Area(math.Copysign(0, -1)),
	} {
		t.Run(name, func(t *testing.T) {
			assert.Nil(t, KpaFromN(4, area), "面积非法必须不可换算，不得用默认 0.64 补位")
			assert.Nil(t, KpaFromN(0, area), "0 N 也一样不可换算：这一格显示的是面积问题，不是压力")
		})
	}
}

// 压力本身是非有限值（帧值可为 NaN，PRD §三 计算顺序明写）：不得洗成合法读数。
// 归零刻意不用 math.Max —— 它对 (0, NaN) 返回 0。
func TestT508_KpaFromN_NonFinitePressureFailsClosed(t *testing.T) {
	area := t508Area(0.64)
	assert.Nil(t, KpaFromN(math.NaN(), area), "NaN 归零后仍是 NaN：math.Max(0, NaN) 会假绿")
	assert.Nil(t, KpaFromN(math.Inf(1), area), "+Inf 换算出来是 Infinity，稿面禁止显示")
}

// 写侧只挡非正数，面积极小是「能配出来」的配置；极端配置把结果推出 int32 时
// 溢出成负数是假读数，不如 --（本格不设面积下界，理由见 KpaFromN 注释）。
func TestT508_KpaFromN_OverflowFailsClosed(t *testing.T) {
	assert.Greater(t, t508Calc(6, 1e-9), float64(math.MaxInt32), "先自证这一格真的超出 int32，否则下面的断言是空跑")
	assert.Nil(t, KpaFromN(6, t508Area(1e-9)), "换算结果超出可显示整数范围时不可换算")
	// 上界另一侧：int32 之内仍要正常出数（护栏别宽到把合法读数一起挡掉）
	assert.Equal(t, 15_625_000, t508Kpa(t, 1_000_000, t508Area(0.64)))
	assert.Less(t, t508Calc(1_000_000, 0.64), float64(math.MaxInt32))
}

// t508Calc 绕过护栏拿到未钳位的换算值，只为上面的量级断言服务。
func t508Calc(n, area float64) float64 { return n / area * 10 }

// BuildHeatmap：每点带 kPa，IsMax 与色阶继续按 N（稿面「判色用原始值」同一条原则）。
func TestT508_BuildHeatmap_CarriesKpaPerPoint(t *testing.T) {
	var pts [PointCount]float32
	for i := range pts {
		pts[i] = float32(i) * 0.3 // 0 … 5.7 N，最大点在最后一点
	}
	area := t508Area(0.64)

	hm := BuildHeatmap(pts, area)
	require.Len(t, hm, PointCount)

	maxIdx := -1
	maxN, maxKpa := float64(-1), -1
	for i, p := range hm {
		require.NotNil(t, p.PressureKpa, "%s 应有 kPa 展示值", p.PointID)
		assert.Equal(t, KpaFromN(p.PressureValue, area), p.PressureKpa, "%s kPa 必须等于同点 N 的换算", p.PointID)
		if p.PressureValue > maxN {
			maxN, maxIdx = p.PressureValue, i
		}
		if *p.PressureKpa > maxKpa {
			maxKpa = *p.PressureKpa
		}
	}
	require.NotEqual(t, -1, maxIdx)
	assert.True(t, hm[maxIdx].IsMax, "最大点标记仍按 N 选")
	// half up + 最小 1 钳位对非负值单调 ⇒ 按 N 选出的最大点，其 kPa 也是全场最大。
	// 这条一致性是「同一格两档同为最大点」（§三 末条）的实现依据。
	assert.Equal(t, maxKpa, *hm[maxIdx].PressureKpa, "最大点的 kPa 必须也是全场最大，否则两档会指向不同格")
}

// 面积未配置：热力图每点 kPa 都是 null（不是 0），N 档数值照旧。
// null 要真的序列化成 JSON null —— 前端按「字段为 null 显示 --」渲染。
func TestT508_BuildHeatmap_UnconfiguredAreaIsNullJSON(t *testing.T) {
	var pts [PointCount]float32
	pts[0], pts[7] = 2.0, 4.0

	hm := BuildHeatmap(pts, nil)
	require.Len(t, hm, PointCount)
	for _, p := range hm {
		assert.Nil(t, p.PressureKpa, "%s 未配置面积不得造出 kPa", p.PointID)
	}
	assert.True(t, hm[7].IsMax, "N 档判档不受面积影响")
	assert.InDelta(t, 4.0, hm[7].PressureValue, 1e-9)

	raw, err := json.Marshal(hm[7])
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"pressureKpa":null`)
	assert.NotContains(t, string(raw), `"pressureKpa":0`)
}

// 全零帧（未佩戴）：无最大点，但每点 kPa 是 0 而不是 null —— 0 是「读到零压力」的真读数。
func TestT508_BuildHeatmap_AllZeroFrame(t *testing.T) {
	var pts [PointCount]float32
	hm := BuildHeatmap(pts, t508Area(0.64))
	for _, p := range hm {
		require.NotNil(t, p.PressureKpa)
		assert.Equal(t, 0, *p.PressureKpa)
		assert.False(t, p.IsMax, "全零不得凭空标出最大点（T325 口径）")
	}
}
