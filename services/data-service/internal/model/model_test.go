package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPointIDAndLabel(t *testing.T) {
	assert.Equal(t, "P01", PointID(0))
	assert.Equal(t, "P20", PointID(19))

	row, col, label := PointLabel(0)
	assert.Equal(t, 1, row)
	assert.Equal(t, 1, col)
	assert.Equal(t, "R1C1", label)

	// P11 = 第 3 行第 1 列（行优先，4×5 矩阵）
	row, col, label = PointLabel(10)
	assert.Equal(t, 3, row)
	assert.Equal(t, 1, col)
	assert.Equal(t, "R3C1", label)
}

func TestPointStatus(t *testing.T) {
	// T733 方案A：PointStatus 入参恒为展示空间 N，mN 阈值（默认 high=5000）经 ToDisplay 换算（high=5N）
	th := DefaultPressureThresholds().ToDisplay() // T203: PressureHighN=5, warning=3.75, critical=5
	assert.Equal(t, "normal", PointStatus(3, th))
	assert.Equal(t, "warning", PointStatus(4, th))
	assert.Equal(t, "critical", PointStatus(6, th))
}

func TestBuildSensorPoints(t *testing.T) {
	var points [PointCount]float32
	points[5] = 50 // P06 critical（本函数入参已是展示空间 N）
	pts := BuildSensorPoints(points, DefaultPressureThresholds().ToDisplay(), nil)
	require.Len(t, pts, PointCount)
	assert.Equal(t, "P06", pts[5].PointID)
	assert.Equal(t, "critical", pts[5].Status)
	assert.Equal(t, "R2C1", pts[5].Label)
	assert.InDelta(t, 50.0, pts[5].PressureValue, 0.001)
	assert.Equal(t, "normal", pts[0].Status)
	assert.Nil(t, pts[5].PressureKpa, "面积未配置（nil）时 kPa 档 fail-closed，不补默认 0.64")
}

// T643 A 路：records 桶的逐点 kPa 与热力图同源（同一个 KpaFromN、同一枚面积），
// 且 Status 分档仍按 N（kPa 只改数值文本，不改判档）。
func TestBuildSensorPoints_kpaSameSourceAsHeatmap(t *testing.T) {
	area := 0.64
	var points [PointCount]float32
	points[5] = 50 // 展示空间 N（T733：mN 帧由调用方 FrameToN 换算后才进这两个 builder）
	th := DefaultPressureThresholds().ToDisplay()
	pts := BuildSensorPoints(points, th, &area)
	hm := BuildHeatmap(points, &area)

	require.Len(t, pts, PointCount)
	for i := range pts {
		assert.Equal(t, KpaFromN(pts[i].PressureValue, &area), pts[i].PressureKpa, "逐点 kPa 必须是 KpaFromN 的现算结果")
		assert.Equal(t, hm[i].PressureKpa, pts[i].PressureKpa, "同帧同面积下两条读路的 kPa 必须逐格相等")
	}
	assert.Equal(t, "critical", pts[5].Status, "判档仍按 N，不受 kPa 影响")

	nilArea := BuildSensorPoints(points, th, nil)
	assert.Nil(t, nilArea[5].PressureKpa, "面积缺失 ⇒ null")
	assert.InDelta(t, nilArea[5].PressureValue, pts[5].PressureValue, 1e-9, "N 值不受面积缺失影响")
}

func TestPressureRecord_MaxPointAndDTO(t *testing.T) {
	rec := &PressureRecord{
		RecordID:  123,
		DeviceID:  "DEV1",
		PatientID: "P1",
	}
	rec.Points[2] = 30200 // P03 最大（T733：库行点值 = mN 存储空间，30.2 N）
	rec.Ts = MinValidTime
	rec.UploadTime = MinValidTime

	assert.Equal(t, "P03", rec.MaxPoint())

	dto := rec.ToDTO(DefaultPressureThresholds(), nil)
	assert.Equal(t, "123", dto.RecordID)
	assert.Equal(t, "DEV1", dto.DeviceID)
	assert.Equal(t, "P1", dto.PatientID)
	assert.Equal(t, "2026-01-01T00:00:00Z", dto.Timestamp)
	assert.Equal(t, "2026-01-01T00:00:00Z", dto.UploadTime)
	require.Len(t, dto.Points, PointCount)
	// T733 方案A：库行 mN → DTO N 的唯一换算发生在 ToDTO（FrameToN，分母 MnPerN）
	assert.InDelta(t, 30.2, dto.Points[2].PressureValue, 1e-6, "DTO 点值必须是 ÷1000 后的 N")
	assert.False(t, dto.Calibrated) // T173：DTO 构造默认未校准，读取侧按校准结果回填
}

// T366：maxPressure 是库侧生成列（raw greatest(p01..p20)）的同源值，不是从校准后的 points 现算的。
// 聚合的佩戴帧判据用的正是这一列，读接口不给它就无法独立复算。
// T733 方案A：本列与点值同为 mN 存储空间，出库 DTO 时同样只 ÷MnPerN 一次；
// 「原样透传」指的是不被 max(points) 覆盖、也不经校准，不再指「数值等于库值」。
func TestPressureRecordDTO_CarriesRawMaxPressure(t *testing.T) {
	rec := &PressureRecord{MaxPressure: 47200, Ts: MinValidTime, UploadTime: MinValidTime} // 47.2 N（mN 存储）
	rec.Points[2] = 30200                                                                  // 点值刻意与生成列不同（30.2 N，同为 mN）

	dto := rec.ToDTO(DefaultPressureThresholds(), nil)
	assert.InDelta(t, 47.2, dto.MaxPressure, 1e-6, "DTO 是库侧列同一物理压力的 N 表达，不用点值覆盖")
	assert.NotEqual(t, 30.2, dto.MaxPressure, "不是 max(points)")
	assert.InDelta(t, 30.2, dto.Points[2].PressureValue, 1e-6, "点值与峰值各换算一次，不重复除")
}

// T366：Redis 回退路径没有库侧列，按同一口径现算，保证两分支的 maxPressure 可比
func TestMaxPointValue(t *testing.T) {
	var p [PointCount]float32
	p[0] = 1.5
	p[7] = 9.9
	assert.InDelta(t, 9.9, MaxPointValue(p), 1e-6)

	var zero [PointCount]float32
	assert.Zero(t, MaxPointValue(zero), "全 0 帧峰值就是 0，不兜底成假值")
}

func TestAppError_Constructors(t *testing.T) {
	e := ErrInvalidParam("bad %s", "points")
	assert.Equal(t, CodeInvalidParam, e.Code)
	assert.Equal(t, 400, e.HTTPStatus)
	assert.Contains(t, e.Error(), "bad points")

	e = ErrBadTimestamp("ts")
	assert.Equal(t, CodeBadTimestamp, e.Code)

	e = ErrDeviceIDMismatch()
	assert.Equal(t, CodeDeviceIDMismatch, e.Code)

	e = ErrDeviceNotFound("D1")
	assert.Equal(t, CodeDeviceNotFound, e.Code)
	assert.Equal(t, 404, e.HTTPStatus)

	e = ErrDeviceUnbound("D1")
	assert.Equal(t, CodeDeviceUnbound, e.Code)

	e = ErrPatientNotFound("P1") // T340
	assert.Equal(t, CodePatientNotFound, e.Code)
	assert.Equal(t, 404, e.HTTPStatus)
	assert.Contains(t, e.Error(), `patient "P1" not found`)

	e = ErrRateLimited(2)
	assert.Equal(t, CodeRateLimited, e.Code)
	assert.Equal(t, 429, e.HTTPStatus)
	assert.Equal(t, 2, e.RetryAfterSec)

	e = ErrQueryParam("q")
	assert.Equal(t, CodeQueryParam, e.Code)

	e = ErrInternal("boom")
	assert.Equal(t, CodeInternal, e.Code)
	assert.Equal(t, 500, e.HTTPStatus)
}

func TestCSTZone(t *testing.T) {
	_, offset := MinValidTime.In(CSTZone()).Zone()
	assert.Equal(t, 8*3600, offset)
}
