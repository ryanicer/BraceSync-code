// Package main — multi-device simulator engine for device chain testing.
// 对齐：docs/ §5 (设备链路测试) · docs/ §9
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

// Frame 单帧压力数据（20 点传感器）
type Frame struct {
	DeviceID  string    `json:"device_id"`
	Timestamp time.Time `json:"timestamp"`
	Pressures [20]int   `json:"pressures"` // p01-p20，单位 kPa
	Battery   int       `json:"battery"`   // 电量百分比
	Wearing   bool      `json:"wearing"`   // 是否在佩戴中
}

// BatchReport 批量补传请求
type BatchReport struct {
	DeviceID string  `json:"device_id"`
	Frames   []Frame `json:"frames"`
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
		Timestamp: time.Now(),
		Battery:   85,
		Wearing:   true,
	}

	if e.faultMode {
		// 故障模式：随机注入异常
		switch rand.Intn(10) {
		case 0:
			// 传感器漂移：某点异常偏高
			frame.Pressures[rand.Intn(20)] = 80 + rand.Intn(40)
		case 1:
			// 电池低电量
			frame.Battery = rand.Intn(10)
		case 2:
			// 佩戴状态不稳定
			frame.Wearing = false
		case 3:
			// 全点归零（传感器故障）
			for i := range frame.Pressures {
				frame.Pressures[i] = 0
			}
		default:
			// 正常数据（含适度波动）
			frame.Pressures = normalPressures()
		}
	} else {
		frame.Pressures = normalPressures()
	}

	return frame
}

// normalPressures generates a normal pressure distribution with slight variation.
func normalPressures() [20]int {
	base := [20]int{12, 15, 18, 20, 22, 19, 16, 14, 13, 11, 10, 12, 14, 16, 18, 20, 17, 15, 13, 11}
	for i := range base {
		// Add ±2 random variation
		base[i] += rand.Intn(5) - 2
		if base[i] < 0 {
			base[i] = 0
		}
	}
	return base
}

func sendFrame(url, deviceID, secret string, frame Frame) error {
	body, _ := json.Marshal(frame)
	ts := time.Now()
	nonce := RandomNonce() // T067：每请求 32 hex nonce

	req, err := http.NewRequest("POST", url+"/api/v1/device/report", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Device-Id", deviceID)
	req.Header.Set("X-Timestamp", fmt.Sprintf("%d", ts.Unix()))
	req.Header.Set("X-Nonce", nonce)
	req.Header.Set("X-Signature", SignDeviceRequest(secret, "POST", "/api/v1/device/report", deviceID, nonce, string(body), ts))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	log.Printf("[%s] frame sent OK (battery=%d%%, wearing=%v)", deviceID, frame.Battery, frame.Wearing)
	return nil
}

// SimulateBackfill generates 7-day backfill frames for device chain testing.
func SimulateBackfill(deviceID string, count int) []Frame {
	frames := make([]Frame, 0, count)
	base := time.Now().Add(-7 * 24 * time.Hour)
	for i := 0; i < count; i++ {
		pressures := normalPressures()
		frames = append(frames, Frame{
			DeviceID:  deviceID,
			Timestamp: base.Add(time.Duration(i) * 30 * time.Minute),
			Pressures: pressures,
			Battery:   90 - (i % 20),
			Wearing:   true,
		})
	}
	return frames
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
