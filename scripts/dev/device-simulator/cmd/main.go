// BraceSync 设备模拟器 — 模拟支具 WiFi HTTPS 上报
// 对齐：docs/ · docs/ §5
//
// 模式：
//   single    — 发送单帧数据
//   batch     — 模拟 7 天补传风暴
//   timed     — N 台设备定时周期性上报
//   fault     — 故障模拟（传感器漂移/电池低电量/佩戴中断等）
//
// 用法：
//   go run ./cmd -mode=single -device=DEV001 -secret=xxx -url=http://localhost:8080
//   go run ./cmd -mode=timed -devices=DEV001,DEV002,DEV003 -rate=30s
//   go run ./cmd -mode=fault -device=DEV001
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	mode := flag.String("mode", "single", "运行模式：single / batch / timed / fault")
	deviceID := flag.String("device", "DEV_SIM_001", "设备 ID（single/batch/fault 模式）")
	devices := flag.String("devices", "", "多设备 ID 列表，逗号分隔（timed 模式）")
	secret := flag.String("secret", "test_secret", "设备签名密钥")
	url := flag.String("url", "http://localhost:8080", "网关地址")
	count := flag.Int("count", 336, "batch 模式帧数 / timed 模式上报次数")
	rate := flag.Duration("rate", 30*time.Second, "timed 模式上报间隔")
	flag.Parse()

	log.Printf("device-simulator starting: mode=%s", *mode)

	switch *mode {
	case "single":
		runSingle(*deviceID, *secret, *url)

	case "batch":
		runBatch(*deviceID, *secret, *url, *count)

	case "timed":
		deviceList := parseDeviceList(*devices, *deviceID)
		runTimed(deviceList, *secret, *url, *rate)

	case "fault":
		runFault(*deviceID, *secret, *url)

	default:
		log.Fatalf("unknown mode: %s (use single/batch/timed/fault)", *mode)
	}
}

func runSingle(deviceID, secret, baseURL string) {
	frame := Frame{
		DeviceID:  deviceID,
		Timestamp: time.Now(),
		Pressures: normalPressures(),
		Battery:   85,
		Wearing:   true,
	}
	if err := sendFrame(baseURL, deviceID, secret, frame); err != nil {
		log.Fatalf("single send failed: %v", err)
	}
	log.Println("single frame sent OK")
}

func runBatch(deviceID, secret, baseURL string, count int) {
	frames := SimulateBackfill(deviceID, count)
	batch := BatchReport{DeviceID: deviceID, Frames: frames}
	body, _ := json.Marshal(batch)
	ts := time.Now()
	nonce := RandomNonce() // T067：每请求 32 hex nonce

	req := mustNewRequest("POST", baseURL+"/api/v1/device/report/batch", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Device-Id", deviceID)
	req.Header.Set("X-Timestamp", fmt.Sprintf("%d", ts.Unix()))
	req.Header.Set("X-Nonce", nonce)
	req.Header.Set("X-Signature", SignDeviceRequest(secret, "POST", "/api/v1/device/report/batch", deviceID, nonce, string(body), ts))

	resp, err := httpClient().Do(req)
	if err != nil {
		log.Fatalf("batch send failed: %v", err)
	}
	defer resp.Body.Close()
	log.Printf("[%s] batch sent: %d frames, status=%d", deviceID, count, resp.StatusCode)
}

func runTimed(deviceList []string, secret, baseURL string, rate time.Duration) {
	configs := make([]SimConfig, len(deviceList))
	for i, id := range deviceList {
		configs[i] = SimConfig{
			DeviceID: id,
			Secret:   secret,
			BaseURL:  baseURL,
		}
	}

	engine := NewSimEngine(configs, rate)
	engine.Start()

	// 等待 Ctrl+C 停止
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	engine.Stop()
	log.Println("timed mode stopped by signal")
}

func runFault(deviceID, secret, baseURL string) {
	configs := []SimConfig{{DeviceID: deviceID, Secret: secret, BaseURL: baseURL}}
	engine := NewSimEngine(configs, 10*time.Second)
	engine.EnableFaultMode()
	engine.Start()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	engine.Stop()
	log.Println("fault mode stopped by signal")
}

func parseDeviceList(devicesArg, fallback string) []string {
	if devicesArg != "" {
		return strings.Split(devicesArg, ",")
	}
	if fallback != "" {
		return []string{fallback}
	}
	return []string{"DEV_SIM_001"}
}
