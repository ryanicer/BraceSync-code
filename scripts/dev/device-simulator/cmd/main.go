// BraceSync 设备模拟器 — 模拟支具 WiFi HTTPS 上报
// 对齐：docs/ · docs/ §5
//
// 模式：
//   single    — 发送单帧数据
//   batch     — 模拟 7 天补传风暴
//   timed     — N 台设备定时周期性上报
//   fault     — 故障模拟（传感器漂移/电池低电量/设备自报故障码等）
//   probe     — 不发真帧，只探两条上报路径存不存在（404 = 路径写错，Fatal）
//
// 用法：
//   go run ./cmd -mode=single -device=DEV001 -secret=xxx -url=http://localhost:8080
//   go run ./cmd -mode=timed -devices=DEV001,DEV002,DEV003 -rate=30s
//   go run ./cmd -mode=fault -device=DEV001
//   go run ./cmd -mode=probe -url=http://localhost:8080
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	mode := flag.String("mode", "single", "运行模式：single / batch / timed / fault / probe")
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

	case "probe":
		probePaths(*url)

	default:
		log.Fatalf("unknown mode: %s (use single/batch/timed/fault/probe)", *mode)
	}
}

func runSingle(deviceID, secret, baseURL string) {
	frame := Frame{
		DeviceID:  deviceID,
		Timestamp: time.Now().Unix(),
		Points:    normalPressures(),
		Battery:   85,
		Firmware:  defaultFirmware,
	}
	if err := sendFrame(baseURL, deviceID, secret, frame); err != nil {
		log.Fatalf("single send failed: %v", err)
	}
	log.Println("single frame sent OK")
}

// runBatch 按云端单请求上限分批发送（协议 §4.2：7 天满缓存 336 帧分 4 批，按时间升序）。
func runBatch(deviceID, secret, baseURL string, count int) {
	frames := SimulateBackfill(count)
	for _, chunk := range chunkFrames(frames, MaxBatchFrames) {
		if err := sendBatchChunk(deviceID, secret, baseURL, chunk); err != nil {
			log.Fatalf("batch send failed: %v", err)
		}
	}
}

func sendBatchChunk(deviceID, secret, baseURL string, frames []BatchFrame) error {
	batch := BatchReport{DeviceID: deviceID, Frames: frames, Firmware: defaultFirmware}
	body, _ := json.Marshal(batch)
	ts := time.Now()
	nonce := RandomNonce() // T067：每请求 32 hex nonce

	req := mustNewRequest("POST", baseURL+PathBatch, body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Device-Id", deviceID)
	req.Header.Set("X-Timestamp", fmt.Sprintf("%d", ts.Unix()))
	req.Header.Set("X-Nonce", nonce)
	req.Header.Set("X-Signature", SignDeviceRequest(secret, "POST", PathBatch, deviceID, nonce, string(body), ts))

	resp, err := httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("transport: %w", err)
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return fmt.Errorf("read response: %w", readErr)
	}
	log.Printf("[%s] batch sent: %d frames, status=%d body=%s",
		deviceID, len(frames), resp.StatusCode, strings.TrimSpace(string(raw)))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	return nil
}

// probePaths 不带签名的路径自检：只问「这条路径存不存在」，不问「这台设备能不能签」。
// 404 = 路径与网关注册不一致（T551 S-1 那一形），非 404 = 路径在（预期 401/20401 要签名）。
// 两条路径任何一条回 404 就 Fatal，让「模拟器空跑」这件事在 CI/手工都能当场响。
func probePaths(baseURL string) {
	for _, p := range []string{PathSingle, PathBatch} {
		req := mustNewRequest("POST", baseURL+p, []byte(`{"points":[]}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Device-Id", "PATH_PROBE")
		resp, err := httpClient().Do(req)
		if err != nil {
			log.Fatalf("probe %s: transport error: %v", p, err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		log.Printf("probe POST %s -> status=%d body=%s", p, resp.StatusCode, strings.TrimSpace(string(raw)))
		if resp.StatusCode == http.StatusNotFound {
			log.Fatalf("probe %s: 404 —— 模拟器路径与网关注册不一致（注册的是 %s 与 %s）",
				p, PathSingle, PathBatch)
		}
	}
	log.Printf("probe OK: %d paths, none 404", 2)
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
