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
	th := DefaultPressureThresholds() // T203: PressureHighN=5, warning=3.75, critical=5
	assert.Equal(t, "normal", PointStatus(3, th))
	assert.Equal(t, "warning", PointStatus(4, th))
	assert.Equal(t, "critical", PointStatus(6, th))
}

func TestBuildSensorPoints(t *testing.T) {
	var points [PointCount]float32
	points[5] = 50 // P06 critical
	pts := BuildSensorPoints(points, DefaultPressureThresholds())
	require.Len(t, pts, PointCount)
	assert.Equal(t, "P06", pts[5].PointID)
	assert.Equal(t, "critical", pts[5].Status)
	assert.Equal(t, "R2C1", pts[5].Label)
	assert.InDelta(t, 50.0, pts[5].PressureValue, 0.001)
	assert.Equal(t, "normal", pts[0].Status)
}

func TestPressureRecord_MaxPointAndDTO(t *testing.T) {
	rec := &PressureRecord{
		RecordID:  123,
		DeviceID:  "DEV1",
		PatientID: "P1",
	}
	rec.Points[2] = 30.2 // P03 最大
	rec.Ts = MinValidTime
	rec.UploadTime = MinValidTime

	assert.Equal(t, "P03", rec.MaxPoint())

	dto := rec.ToDTO(DefaultPressureThresholds())
	assert.Equal(t, "123", dto.RecordID)
	assert.Equal(t, "DEV1", dto.DeviceID)
	assert.Equal(t, "P1", dto.PatientID)
	assert.Equal(t, "2026-01-01T00:00:00Z", dto.Timestamp)
	assert.Equal(t, "2026-01-01T00:00:00Z", dto.UploadTime)
	require.Len(t, dto.Points, PointCount)
	assert.False(t, dto.Calibrated) // T173：DTO 构造默认未校准，读取侧按校准结果回填
}

// T366：maxPressure 是库侧生成列（raw greatest(p01..p20)）的同源值，不是从校准后的 points 现算的。
// 聚合的佩戴帧判据用的正是这一列，读接口不给它就无法独立复算。
func TestPressureRecordDTO_CarriesRawMaxPressure(t *testing.T) {
	rec := &PressureRecord{MaxPressure: 47.2, Ts: MinValidTime, UploadTime: MinValidTime}
	rec.Points[2] = 30.2 // 点值刻意与生成列不同（库里点值可能是 ÷1000 后的另一层）

	dto := rec.ToDTO(DefaultPressureThresholds())
	assert.InDelta(t, 47.2, dto.MaxPressure, 1e-6, "DTO 原样透出库侧列，不用点值覆盖")
	assert.NotEqual(t, 30.2, dto.MaxPressure, "不是 max(points)")
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
