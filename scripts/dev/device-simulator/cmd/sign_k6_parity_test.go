// Package main — k6 压测脚本的签名口径对拍（T566 格 3）
package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// 固定入参与 scripts/dev/loadtest/sign-parity-check.mjs 逐字一致；EXPECTED 那颗 hex 由该尺在
// dell-tower 现跑打出（node scripts/dev/loadtest/sign-parity-check.mjs）。Go 侧 6 行签名串格式
// 一旦改动（字段顺序、分隔符、编码），本测试即红，同时提示去重跑那把 JS 尺。
const (
	k6ParityDevice = "DEV_PARITY_001"
	k6ParityBody   = `{"device_id":"DEV_PARITY_001","timestamp":1723028400,"points":[1200],"battery":85,"firmware":"v1.2.0"}`
	k6ParityNonce  = "0123456789abcdef0123456789abcdef"
	k6ParityJSHex  = "8cc7f633c16a60ffceddf9dd4886fcf95fb972ee4ceb21c819420e10ed85c46a"
)

func TestSignParityWithK6Helper(t *testing.T) {
	// "test_secret" 即模拟器 main.go 的 -secret 默认值，也是 k6 侧 device-sign.js 的 DEFAULT_SECRET
	got := SignDeviceRequest("test_secret", "POST", PathSingle, k6ParityDevice, k6ParityNonce, k6ParityBody, time.Unix(1723028400, 0))
	assert.Equal(t, k6ParityJSHex, got, "Go 侧签名与 k6 脚本 helper 漂移，重跑 node scripts/dev/loadtest/sign-parity-check.mjs")
}
