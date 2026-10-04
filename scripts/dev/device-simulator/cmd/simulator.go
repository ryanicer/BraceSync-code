// Package main — multi-device simulator engine for device chain testing.
// 对齐：docs/ §5 (设备链路测试) · docs/ §9
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"
)

// 上报路径 = gateway 设备签名组注册的这两条（services/gateway/cmd/server/proxy_services.go
// 的 deviceReportRoutes）。历史上这里写的是 /device/report 与 /device/report/batch，
// 现网实测前者 404 page not found、后者 401/20401 ⇒ 模拟器一直在空跑（T551 S-1，T562 M1 修复）。
const (
	PathSingle = "/api/v1/device/records"
	PathBatch  = "/api/v1/device/records/batch"
)

// MnPerN 设备侧量纲口径：points 单位 = mN，云端入口统一 ÷1000 归一为 N 后落库/判档
// （services/data-service/internal/model/model.go 的 SingleFrameRequest.Points 注释，T173 D1 裁定）。
const MnPerN = 1000

// defaultFirmware 上报体必填字段 firmware 的默认值（云端仅在版本变化时 UPDATE）。
const defaultFirmware = "v1.2.0"

// MaxBatchFrames 单请求补传帧数上限。与云端 model.MaxBatchFrames 同值
// （services/data-service/internal/model/model.go 里的那个常量）——两个模块不同 go.work，
// 不跨模块引依赖，改动必须两侧同步，否则整批会被 400 拒（旧模拟器一请求发 336 帧即此形）。
const MaxBatchFrames = 100

// Frame 单帧实时上报请求体，对齐 data-service 的 SingleFrameRequest。
// 注意：没有 wearing 字段——是否佩戴由云端按 max(points) 与阈值推导，设备侧不报。
type Frame struct {
	DeviceID  string    `json:"device_id,omitempty"` // 生产以网关注入的 X-Device-Id 为准
	Timestamp int64     `json:"timestamp"`           // 采集时刻，Unix 秒
	Points    []float64 `json:"points"`              // P01–P20 顺序，单位 mN
	Battery   int       `json:"battery"`
	Firmware  string    `json:"firmware"`
	WifiRSSI  *int      `json:"wifi_rssi,omitempty"`
	FaultCode int       `json:"fault_code,omitempty"`
}

// BatchFrame 补传批次里的单帧，对齐 data-service 的 BatchFrame（无 device_id/firmware）。
type BatchFrame struct {
	Timestamp int64     `json:"timestamp"`
	Points    []float64 `json:"points"`
	Battery   int       `json:"battery"`
	FaultCode int       `json:"fault_code,omitempty"`
}

// BatchReport 批量补传请求体，对齐 data-service 的 BatchRequest（单请求 ≤100 帧）。
type BatchReport struct {
	DeviceID string       `json:"device_id,omitempty"`
	Frames   []BatchFrame `json:"frames"`
	Firmware string       `json:"firmware"`
}

// SimConfig 模拟器配置
type SimConfig struct {
	DeviceID string
	Secret   string
	BaseURL  string
}

// SimEngine 多设备模拟引擎
type SimEngine struct {
	devices    []SimConfig
	stopCh     chan struct{}
	wg         sync.WaitGroup
	faultMode  bool
	reportRate time.Duration // 上报间隔
}

// NewSimEngine creates a new multi-device simulator.
func NewSimEngine(devices []SimConfig, reportRate time.Duration) *SimEngine {
	return &SimEngine{
		devices:    devices,
		stopCh:     make(chan struct{}),
		reportRate: reportRate,
	}
}

// EnableFaultMode enables fault simulation (sensor errors, battery drain, etc.)
func (e *SimEngine) EnableFaultMode() {
	e.faultMode = true
}

// Start begins timed reporting for all devices.
func (e *SimEngine) Start() {
	for i := range e.devices {
		dev := &e.devices[i]
		e.wg.Add(1)
		go e.reportLoop(dev)
	}
	log.Printf("simulator: %d devices started, rate=%v, faultMode=%v",
		len(e.devices), e.reportRate, e.faultMode)
}

// Stop gracefully stops all reporting goroutines.
func (e *SimEngine) Stop() {
	close(e.stopCh)
	e.wg.Wait()
	log.Println("simulator: all devices stopped")
}

func (e *SimEngine) reportLoop(dev *SimConfig) {
	defer e.wg.Done()
	ticker := time.NewTicker(e.reportRate)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			frame := e.generateFrame(dev.DeviceID)
			if err := sendFrame(dev.BaseURL, dev.DeviceID, dev.Secret, frame); err != nil {
				log.Printf("[%s] send error: %v", dev.DeviceID, err)
			}
		}
	}
}

func (e *SimEngine) generateFrame(deviceID string) Frame {
	frame := Frame{
		DeviceID:  deviceID,
		Timestamp: time.Now().Unix(),
		Battery:   85,
		Firmware:  defaultFirmware,
		Points:    normalPressures(),
	}

	if e.faultMode {
		// 故障模式：随机注入异常
		switch rand.Intn(10) {
		case 0:
			// 传感器漂移：某点异常偏高（超云端现行高阈 5N）
			frame.Points[rand.Intn(len(frame.Points))] = 8000 + float64(rand.Intn(4000))
		case 1:
			// 电池低电量
			frame.Battery = rand.Intn(10)
		case 2:
			// 设备自报故障码（旧写法是 wearing=false，但云端不收 wearing 字段，
			// 佩戴态由 max(points) 与阈值推导，故障态在协议里的表达就是 fault_code）
			frame.FaultCode = 3
		case 3:
			// 全点归零（传感器故障/离体）
			frame.Points = make([]float64, len(frame.Points))
		}
	}

	return frame
}

// normalPressures 生成一帧正常压力分布（单位 mN，见 MnPerN 口径），带 ±200 mN 抖动。
func normalPressures() []float64 {
	baseN := [20]float64{1.2, 1.5, 1.8, 2.0, 2.2, 1.9, 1.6, 1.4, 1.3, 1.1, 1.0, 1.2, 1.4, 1.6, 1.8, 2.0, 1.7, 1.5, 1.3, 1.1}
	out := make([]float64, len(baseN))
	for i, v := range baseN {
		n := v*float64(MnPerN) + float64(rand.Intn(401)-200)
		if n < 0 {
			n = 0
		}
		out[i] = n
	}
	return out
}

func sendFrame(url, deviceID, secret string, frame Frame) error {
	body, _ := json.Marshal(frame)
	ts := time.Now()
	nonce := RandomNonce() // T067：每请求 32 hex nonce

	req, err := http.NewRequest("POST", url+PathSingle, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Device-Id", deviceID)
	req.Header.Set("X-Timestamp", fmt.Sprintf("%d", ts.Unix()))
	req.Header.Set("X-Nonce", nonce)
	req.Header.Set("X-Signature", SignDeviceRequest(secret, "POST", PathSingle, deviceID, nonce, string(body), ts))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return fmt.Errorf("read response: %v", readErr)
	}
	if resp.StatusCode != http.StatusOK {
		// 原始响应体一起报出去：路径写错时的 404 page not found 与签名不对时的 401/20401
		// 靠这段正文分型（T562 M1 判据①要求附原始响应）。
		return fmt.Errorf("unexpected status: %d body=%s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	log.Printf("[%s] frame sent OK (battery=%d%%, fault_code=%d)", deviceID, frame.Battery, frame.FaultCode)
	return nil
}

// SimulateBackfill generates 7-day backfill frames for device chain testing.
// 返回补传帧（无 device_id：批次请求体在 BatchReport 层带上），按时间升序。
func SimulateBackfill(count int) []BatchFrame {
	frames := make([]BatchFrame, 0, count)
	base := time.Now().Add(-7 * 24 * time.Hour)
	for i := 0; i < count; i++ {
		frames = append(frames, BatchFrame{
			Timestamp: base.Add(time.Duration(i) * 30 * time.Minute).Unix(),
			Points:    normalPressures(),
			Battery:   90 - (i % 20),
		})
	}
	return frames
}

// chunkFrames 按单请求上限切批（协议 §4.2：336 帧分 4 批，按时间升序发送）。
// 云端对超限批次的读数是 400 frames count N exceeds 100，不是部分成功。
func chunkFrames(frames []BatchFrame, size int) [][]BatchFrame {
	if size <= 0 {
		size = MaxBatchFrames
	}
	chunks := make([][]BatchFrame, 0, (len(frames)+size-1)/size)
	for start := 0; start < len(frames); start += size {
		end := start + size
		if end > len(frames) {
			end = len(frames)
		}
		chunks = append(chunks, frames[start:end])
	}
	return chunks
}

// mustNewRequest creates a new HTTP request or panics.
func mustNewRequest(method, url string, body []byte) *http.Request {
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		panic(fmt.Sprintf("mustNewRequest: %v", err))
	}
	return req
}

// httpClient returns the shared HTTP client for simulator use.
func httpClient() *http.Client {
	return http.DefaultClient
}
