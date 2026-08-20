// Package model data-service 领域模型与 API DTO 定义
//
// 对齐：docs/ §4（设备上报接口）
//
//	docs/ / getPatientRealtime）
//	docs/ §3.5 / §4.3 / §4.7
package model

import (
	"fmt"
	"time"
)

// PointCount 压力采集点位数（P01–P20）
const PointCount = 20

// 时间合法性约束（device-protocol.md §4.1）：
// 云端校验 timestamp 须落在 [2026-01-01T00:00:00Z, 服务器时间+1h]，超出返回 20402
var (
	// MinValidTime 合法 timestamp 下界（防时钟故障污染按月分区表）
	MinValidTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// MaxFutureDrift 合法 timestamp 上界相对服务器时间的偏移
	MaxFutureDrift = time.Hour
	// BackfillMaxAge 补传帧最大年龄（设备离线缓存上限 7 天，协议 §6）
	BackfillMaxAge = 7 * 24 * time.Hour
)

// WearingThresholdN 佩戴判定阈值（PRD §8.1：帧 max_pressure > 0.5N 视为佩戴帧）
const WearingThresholdN = 0.5

// cstZone 业务切日时区（架构 §3.5：业务切日/定时任务按 Asia/Shanghai）。
// 用固定偏移避免容器缺 tzdata 导致 LoadLocation 失败。
var cstZone = time.FixedZone("Asia/Shanghai", 8*3600)

// CSTZone 返回业务切日时区（Asia/Shanghai，UTC+8）
func CSTZone() *time.Location { return cstZone }

// ─────────────────────────────────────────────────────────────
// 错误码（device-protocol.md §4.4 设备域 2xxxx；架构 §3.5 分段）
// ─────────────────────────────────────────────────────────────

const (
	CodeOK               = 0
	CodeInvalidParam     = 20400 // 参数非法（含 points 长度 != 20、批量 >100 帧）
	CodeBadTimestamp     = 20402 // timestamp 超出合法区间（含补传 >7 天时间窗）
	CodeDeviceIDMismatch = 20403 // X-Device-Id 头与请求体 device_id 不一致
	CodeDeviceNotFound   = 20404 // device_id 未注册
	CodeDeviceUnbound    = 20409 // 设备未绑定患者
	CodeRateLimited      = 20429 // 限流（设备按 Retry-After 退避）
	CodeQueryParam       = 30001 // 数据域：查询参数非法
	CodeInternal         = 90001 // 系统内部错误
)

// AppError 业务错误：携带统一响应 code 与建议 HTTP 状态
type AppError struct {
	Code          int    `json:"code"`
	Message       string `json:"message"`
	HTTPStatus    int    `json:"-"`
	RetryAfterSec int    `json:"-"` // 仅 20429 使用
}

func (e *AppError) Error() string { return fmt.Sprintf("code=%d: %s", e.Code, e.Message) }

func newAppError(code, httpStatus int, format string, args ...any) *AppError {
	return &AppError{Code: code, HTTPStatus: httpStatus, Message: fmt.Sprintf(format, args...)}
}

func ErrInvalidParam(format string, args ...any) *AppError {
	return newAppError(CodeInvalidParam, 400, format, args...)
}

func ErrBadTimestamp(format string, args ...any) *AppError {
	return newAppError(CodeBadTimestamp, 400, format, args...)
}

func ErrDeviceIDMismatch() *AppError {
	return newAppError(CodeDeviceIDMismatch, 400, "X-Device-Id header does not match body device_id")
}

func ErrDeviceNotFound(deviceID string) *AppError {
	return newAppError(CodeDeviceNotFound, 404, "device %q not registered", deviceID)
}

func ErrDeviceUnbound(deviceID string) *AppError {
	return newAppError(CodeDeviceUnbound, 400, "device %q not bound to any patient", deviceID)
}

func ErrRateLimited(retryAfterSec int) *AppError {
	e := newAppError(CodeRateLimited, 429, "rate limited")
	e.RetryAfterSec = retryAfterSec
	return e
}

func ErrQueryParam(format string, args ...any) *AppError {
	return newAppError(CodeQueryParam, 400, format, args...)
}

func ErrInternal(format string, args ...any) *AppError {
	return newAppError(CodeInternal, 500, format, args...)
}

// ─────────────────────────────────────────────────────────────
// 设备上报 DTO（device-protocol.md §4.1 / §4.2）
// ─────────────────────────────────────────────────────────────

// SingleFrameRequest 单帧实时上报请求体
type SingleFrameRequest struct {
	// DeviceID 仅为 gateway 身份头未就绪时的联调回退；
	// 生产以 gateway 注入的 X-Device-Id 为准（验签归 gateway，本服务不越权）。
	DeviceID  string    `json:"device_id,omitempty"`
	Timestamp int64     `json:"timestamp"` // 采集时刻，Unix 秒
	Points    []float64 `json:"points"`    // 20 点压力值（P01–P20 顺序），单位 N
	Battery   int       `json:"battery"`
	Firmware  string    `json:"firmware"`
	WifiRSSI  *int      `json:"wifi_rssi,omitempty"`
	FaultCode int       `json:"fault_code,omitempty"`
}

// BatchFrame 补传批次中的单帧
type BatchFrame struct {
	Timestamp int64     `json:"timestamp"`
	Points    []float64 `json:"points"`
	Battery   int       `json:"battery"`
	FaultCode int       `json:"fault_code,omitempty"`
}

// BatchRequest 批量补传请求体（单请求 ≤100 帧）
type BatchRequest struct {
	DeviceID string       `json:"device_id,omitempty"` // 同 SingleFrameRequest.DeviceID
	Frames   []BatchFrame `json:"frames"`
	Firmware string       `json:"firmware"`
}

// MaxBatchFrames 单请求补传帧数上限（协议 §4.2）
const MaxBatchFrames = 100

// DeviceConfig 云端配置捎带下发（协议 §4.1：唯一的配置下发通道）
type DeviceConfig struct {
	IntervalMinutes int `json:"interval_minutes"`
	ConfigVersion   int `json:"config_version"`
}

// SingleFrameResponse 单帧上报成功响应 data
type SingleFrameResponse struct {
	RecordID   string       `json:"record_id"`
	Duplicated bool         `json:"duplicated"` // 幂等命中：重复帧未重复落库
	Config     DeviceConfig `json:"config"`
}

// RejectedFrame 补传批次中业务校验失败的帧（协议 §4.2 部分成功语义）
type RejectedFrame struct {
	Index  int    `json:"index"`
	Code   int    `json:"code"`
	Reason string `json:"reason"`
}

// BatchResponse 批量补传成功响应 data
type BatchResponse struct {
	Accepted   int             `json:"accepted"`
	Duplicated int             `json:"duplicated"`
	Rejected   []RejectedFrame `json:"rejected"`
	Config     DeviceConfig    `json:"config"`
}

// ─────────────────────────────────────────────────────────────
// 领域实体与查询 DTO（对齐 shared-types PressureRecord / SensorPoint）
// ─────────────────────────────────────────────────────────────

// PressureRecord pressure_records 表行
type PressureRecord struct {
	RecordID    int64
	DeviceID    string
	PatientID   string
	Ts          time.Time
	Points      [PointCount]float32
	MaxPressure float32 // DB 生成列
	UploadTime  time.Time
}

// MaxPoint 返回最大压力点位编号（如 "P06"）；并列取首个
func (r *PressureRecord) MaxPoint() string {
	idx := 0
	for i := 1; i < PointCount; i++ {
		if r.Points[i] > r.Points[idx] {
			idx = i
		}
	}
	return PointID(idx)
}

// PointID 点位下标（0 起）转点位编号（P01–P20）
func PointID(i int) string { return fmt.Sprintf("P%02d", i+1) }

// PointLabel 点位下标转行优先标签（PRD §8.3：Pn = RrCc，4 行 5 列）
func PointLabel(i int) (row, col int, label string) {
	row, col = i/5+1, i%5+1
	return row, col, fmt.Sprintf("R%dC%d", row, col)
}

// 压力展示分级阈值（仅用于前端 status 渲染，非告警阈值）
const (
	pressureWarningN  = 33.75 // 0.75 × 压力偏高默认阈值 45N
	pressureCriticalN = 45.0  // 与告警引擎 pressure_high 默认阈值一致
)

// PointStatus 依据压力值返回展示状态
func PointStatus(v float32) string {
	switch {
	case float64(v) >= pressureCriticalN:
		return "critical"
	case float64(v) >= pressureWarningN:
		return "warning"
	default:
		return "normal"
	}
}

// SensorPoint 对齐 shared-types SensorPoint
type SensorPoint struct {
	PointID       string  `json:"pointId"`
	Row           int     `json:"row"`
	Col           int     `json:"col"`
	Label         string  `json:"label"`
	PressureValue float64 `json:"pressureValue"`
	Status        string  `json:"status"`
}

// BuildSensorPoints 将 20 点原始值转为前端 SensorPoint 数组
func BuildSensorPoints(points [PointCount]float32) []SensorPoint {
	out := make([]SensorPoint, PointCount)
	for i, v := range points {
		row, col, label := PointLabel(i)
		out[i] = SensorPoint{
			PointID:       PointID(i),
			Row:           row,
			Col:           col,
			Label:         label,
			PressureValue: float64(v),
			Status:        PointStatus(v),
		}
	}
	return out
}

// PressureRecordDTO 对齐 shared-types PressureRecord（camelCase）
type PressureRecordDTO struct {
	RecordID   string        `json:"recordId"`
	DeviceID   string        `json:"deviceId"`
	PatientID  string        `json:"patientId"`
	Timestamp  string        `json:"timestamp"` // ISO 8601 UTC
	Points     []SensorPoint `json:"points"`
	UploadTime string        `json:"uploadTime"`
}

// ToDTO 领域实体 → 前端 DTO
func (r *PressureRecord) ToDTO() PressureRecordDTO {
	return PressureRecordDTO{
		RecordID:   fmt.Sprintf("%d", r.RecordID),
		DeviceID:   r.DeviceID,
		PatientID:  r.PatientID,
		Timestamp:  r.Ts.UTC().Format(time.RFC3339),
		Points:     BuildSensorPoints(r.Points),
		UploadTime: r.UploadTime.UTC().Format(time.RFC3339),
	}
}

// HistoryPage 对齐 shared-types PaginatedResponse<PressureRecord>
type HistoryPage struct {
	List     []PressureRecordDTO `json:"list"`
	Total    int64               `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"pageSize"`
}

// ─────────────────────────────────────────────────────────────
// T021：聚合与归档领域模型
// ─────────────────────────────────────────────────────────────

// WearTargetMinutes 默认每日佩戴目标时长（PRD §7A.11：22h = 1320min）
const WearTargetMinutes = 22 * 60

// DailyWearStats daily_wear_stats 表行（架构 §4.4 日聚合）
type DailyWearStats struct {
	PatientID     string
	StatDate      time.Time // 业务时区（Asia/Shanghai）切日，存储为 DATE
	WearMinutes   int
	AvgPressure   float32
	MaxPressure   float32
	MaxPoint      string // P01..P20
	FrameCount    int
	AbnormalCount int
	UpdatedAt     time.Time
}

// HealthReport health_reports 表行（PRD §7A.11 健康报告）
type HealthReport struct {
	ReportID           int64
	PatientID          string
	ReportType         string // "weekly" | "monthly"
	PeriodStart        time.Time
	PeriodEnd          time.Time
	WearComplianceRate float64 // 达标率 %
	AvgPressure        float32
	TrendJudgment      string // "up" | "flat" | "down"
	Suggestion         string
	GenerateTime       time.Time
}

// ArchiveStatus archive_status 表行（T021 冷归档三步走状态追踪）
type ArchiveStatus struct {
	PartitionName string
	PeriodYear    int
	PeriodMonth   int
	Status        string // pending | exported | verified | cleaned | failed
	RowCount      int64
	Checksum      string
	ExportPath    string
	ErrorMessage  string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// RealtimeSnapshot 对齐 api-contracts.ts getPatientRealtime 返回结构
type RealtimeSnapshot struct {
	Status          string              `json:"status"` // online / offline / abnormal
	TodayHours      float64             `json:"todayHours"`
	MaxPressure     float64             `json:"maxPressure"`
	MaxPoint        string              `json:"maxPoint"`
	Events          int                 `json:"events"` // 今日异常值
	PressureRecords []PressureRecordDTO `json:"pressureRecords"`
	Alerts          []any               `json:"alerts"` // 今日告警摘要，明细由 alert-service 提供
}

// Dashboard 常量 (shared between service + integration tests)
const (
	// RankingWindowDays 排行/趋势/分布固定近 7 日窗口 (dashboard.go:36, repo/integration tests)
	RankingWindowDays = 7
)
