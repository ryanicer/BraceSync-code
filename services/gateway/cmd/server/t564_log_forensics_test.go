// T564 取证插桩（网关侧）：20402 拒签那一行要能回答「设备说的是哪一刻、差了几秒、
// 四个头名在不在场」，同时不改变响应体形状、不把签名与 nonce 的值写进日志。
//
// 与 t550_device_time_test.go 不重叠：那一支锁的是「行为」（超差设备能否拿到校时），
// 这一支锁的是「读数」（被拒那一刻留下了什么证据）。
package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/testhelper"
)

const (
	t564Secret = "dev-secret-abc"
	t564Body   = `{"device_id":"DEV-SIG-001","pressures":[10,20]}`
)

// t564Rejected 发一次注定 20402 的上报，返回该请求留下的那条日志行。
// 偏差秒数留给各用例自己断（夹具与验签各读一次 wall clock，只能按容差比）。
func t564Rejected(t *testing.T, gwURL string, hdrs map[string]string) map[string]interface{} {
	t.Helper()
	capture := t564Capture(t)
	code, body := httpDoFull(t, http.MethodPost, gwURL+"/api/v1/device/records", t564Body, hdrs)
	require.Equal(t, http.StatusUnauthorized, code, "本夹具只喂 20402 那一支：%s", body)
	require.Contains(t, body, `"code":20402`)

	return t564EntryByCode(t, capture, 20402)
}

// t564Capture 起一路日志捕获（必须在网关起好之后再开，否则注册行会混进来）。
func t564Capture(t *testing.T) *testhelper.LogCaptureHook {
	t.Helper()
	return t464CaptureLogs(t)
}

// t564EntryByCode 按码取那条日志行，并把「不止一条 / 一条都没有」都判红：
// 报 0 时同屏给出实际捕获到的行数，避免把覆盖面读数当成「没有」。
func t564EntryByCode(t *testing.T, capture *testhelper.LogCaptureHook, code int) map[string]interface{} {
	t.Helper()
	entries := capture.Entries()
	var hits []map[string]interface{}
	for _, entry := range entries {
		if got, ok := entry["code"].(float64); ok && int(got) == code {
			hits = append(hits, entry)
		}
	}
	require.Len(t, hits, 1, "捕获面共 %d 行，期望恰好 1 行 code=%d", len(entries), code)
	return hits[0]
}

func t564Gateway(t *testing.T) string {
	t.Helper()
	backend, _ := captureBackend(t)
	gw := startDeviceGateway(t, backend.URL, &fakeSecretsCtx{secrets: map[string]string{"DEV-SIG-001": t564Secret}})
	return gw.URL
}

// TestT564_20402_LineHasDeclaredTimestampAndSignedSkew 项 1 的插桩落点：
// 注入已知偏差（设备钟落后 6 小时），断言日志行原样报出那一条时刻与同一秒数，符号为负。
func TestT564_20402_LineHasDeclaredTimestampAndSignedSkew(t *testing.T) {
	gwURL := t564Gateway(t)
	declared := time.Now().Add(-6 * time.Hour)
	hdrs := deviceHeaders(t564Secret, http.MethodPost, "/api/v1/device/records", t564Body, declared)

	entry := t564Rejected(t, gwURL, hdrs)

	assert.Equal(t, "DEV-SIG-001", entry["device_id"], "被拒的是哪台设备要写在同一行")
	assert.Equal(t, strconv.FormatInt(declared.Unix(), 10), entry["x_timestamp"], "X-Timestamp 原值，不是截断也不是格式化")
	skew, isNumber := entry["skew_sec"].(float64)
	require.True(t, isNumber, "超窗那一支必须报得出数字形的偏差秒数：%v", entry)
	assert.InDelta(t, float64(-6*60*60), skew, 2, "落后六小时要报成负数，符号反了就会把快钟读成慢钟")
	assert.Equal(t, true, entry["ts_present"])
	assert.Equal(t, true, entry["nonce_present"])
	assert.Equal(t, true, entry["sig_present"])
}

// TestT564_20402_LineHasNoCredentialValues 取证项 3 的另一半：nonce 与签名的「有无」入日志，
// 「值」一律不入——这一条是红线，故直接拿三个已知值在捕获面上反查。
func TestT564_20402_LineHasNoCredentialValues(t *testing.T) {
	gwURL := t564Gateway(t)
	declared := time.Now().Add(-24 * time.Hour)
	hdrs := deviceHeaders(t564Secret, http.MethodPost, "/api/v1/device/records", t564Body, declared)

	capture := t564Capture(t)
	code, body := httpDoFull(t, http.MethodPost, gwURL+"/api/v1/device/records", t564Body, hdrs)
	require.Equal(t, http.StatusUnauthorized, code)
	require.Contains(t, body, `"code":20402`)

	lines := fmt.Sprint(capture.Entries())
	assert.NotContains(t, lines, hdrs["X-Signature"], "签名值不得进日志面")
	assert.NotContains(t, lines, t564Secret, "设备密钥不得进日志面")
	assert.NotContains(t, lines, testDeviceNonce, "nonce 值不得进日志面，只报有无")
}

// TestT564_20402_ReportsWhichHeadersAreAbsent 项 3：头名在场集合。
// 验签顺序是 parse ts → window → HMAC，所以超窗这一支在签名缺失时照样先判 20402，
// 正好用来看「被拒那一刻到底带了哪几个头」。
func TestT564_20402_ReportsWhichHeadersAreAbsent(t *testing.T) {
	gwURL := t564Gateway(t)
	declared := time.Now().Add(-6 * time.Hour)
	hdrs := deviceHeaders(t564Secret, http.MethodPost, "/api/v1/device/records", t564Body, declared)
	delete(hdrs, "X-Nonce")
	delete(hdrs, "X-Signature")

	entry := t564Rejected(t, gwURL, hdrs)

	assert.Equal(t, false, entry["nonce_present"])
	assert.Equal(t, false, entry["sig_present"])
	assert.Equal(t, true, entry["ts_present"])
}

// TestT564_20402_UnparseableTimestampHasNoSkewSlot 「没带戳」与「戳带了但对不上」必须分得开：
// 戳解析不出时，skew_sec 那一格整个缺席——留个 0 在那儿会被读成「偏差正好是 0 秒」。
func TestT564_20402_UnparseableTimestampHasNoSkewSlot(t *testing.T) {
	gwURL := t564Gateway(t)
	declared := time.Now().Add(-6 * time.Hour)
	hdrs := deviceHeaders(t564Secret, http.MethodPost, "/api/v1/device/records", t564Body, declared)
	hdrs["X-Timestamp"] = "2026-10-03T12:00:00Z" // 设备把 ISO 串当 Unix 秒发

	entry := t564Rejected(t, gwURL, hdrs)

	assert.NotContains(t, entry, "skew_sec", "偏差没测出来就不该有这一格：%v", entry)
	assert.Equal(t, "2026-10-03T12:00:00Z", entry["x_timestamp"], "原值照落，正是定位真因要的那一位")
	assert.Equal(t, true, entry["ts_present"])
}

// TestT564_20402_MissingTimestampStillLogged 缺 X-Timestamp（头整个不在）也走 20402：
// 这一支 ts_present=false 且无 skew_sec，与上一例合起来才分得开「没带」与「带错形」。
func TestT564_20402_MissingTimestampStillLogged(t *testing.T) {
	gwURL := t564Gateway(t)
	declared := time.Now().Add(-6 * time.Hour)
	hdrs := deviceHeaders(t564Secret, http.MethodPost, "/api/v1/device/records", t564Body, declared)
	delete(hdrs, "X-Timestamp")

	entry := t564Rejected(t, gwURL, hdrs)

	assert.Equal(t, false, entry["ts_present"])
	assert.NotContains(t, entry, "skew_sec")
	assert.Equal(t, "", entry["x_timestamp"])
}

// TestT564_20402_OverlongTimestampBecomesShapeOnly 异常长的头值只报形状不报内容：
// 挡住有人拿这一格灌日志面，也挡住把非法字节截进 JSON 行里（那会毁掉整行）。
func TestT564_20402_OverlongTimestampBecomesShapeOnly(t *testing.T) {
	gwURL := t564Gateway(t)
	hdrs := deviceHeaders(t564Secret, http.MethodPost, "/api/v1/device/records", t564Body, time.Now().Add(-6*time.Hour))
	hdrs["X-Timestamp"] = strings.Repeat("9", 40) // 40 位：既超 int64，也超入日志的长度上限

	entry := t564Rejected(t, gwURL, hdrs)

	assert.NotContains(t, entry, "skew_sec")
	assert.Equal(t, "<len=40>", entry["x_timestamp"])
	assert.Equal(t, true, entry["ts_present"])
}

// TestT564_OtherCodesCarryNoForensics 插桩只落在 20402 那一支：
// 20401（签名不一致）、20404（未注册）与放行路径的日志面形状不变，
// 免得一次取证改动把整个网关的日志面撑大。
func TestT564_OtherCodesCarryNoForensics(t *testing.T) {
	gwURL := t564Gateway(t)
	now := time.Now()

	fakeSig := deviceHeaders(t564Secret, http.MethodPost, "/api/v1/device/records", t564Body, now)
	fakeSig["X-Signature"] = "deadbeef"
	capture := t564Capture(t)
	code, _ := httpDoFull(t, http.MethodPost, gwURL+"/api/v1/device/records", t564Body, fakeSig)
	require.Equal(t, http.StatusUnauthorized, code)
	require.NotEmpty(t, t564EntryByCode(t, capture, 20401))

	capture.Clear()
	unsigned := deviceHeaders(t564Secret, http.MethodPost, "/api/v1/device/records", t564Body, now)
	unsigned["X-Device-Id"] = "DEV-UNKNOWN"
	code, _ = httpDoFull(t, http.MethodPost, gwURL+"/api/v1/device/records", t564Body, unsigned)
	require.Equal(t, http.StatusUnauthorized, code)
	require.NotEmpty(t, t564EntryByCode(t, capture, 20404))

	for _, entry := range capture.Entries() {
		assert.NotContains(t, entry, "skew_sec", "非 20402 的行不进取证面：%v", entry)
		assert.NotContains(t, entry, "x_timestamp")
	}
}

// TestT564_ResponseShapeUnchanged 插桩只加日志面，响应体一字节不变：
// 20402 的响应仍是中文短句 + trace（T464 双通道），不冒出 skew / x_timestamp 这类内部字段。
func TestT564_ResponseShapeUnchanged(t *testing.T) {
	gwURL := t564Gateway(t)
	declared := time.Now().Add(-6 * time.Hour)
	hdrs := deviceHeaders(t564Secret, http.MethodPost, "/api/v1/device/records", t564Body, declared)

	_ = t564Capture(t)
	code, body := httpDoFull(t, http.MethodPost, gwURL+"/api/v1/device/records", t564Body, hdrs)

	require.Equal(t, http.StatusUnauthorized, code)
	assert.NotContains(t, body, "skew")
	assert.NotContains(t, body, "x_timestamp")
	assert.Contains(t, body, `"trace"`)
}

// TestTimestampForLog_GuardsLogFace 形状闸本体的三条边界：正常 Unix 秒原样过、
// 超长只报长度、含控制字符只报形状（控制字符是日志注入的入口）。
func TestTimestampForLog_GuardsLogFace(t *testing.T) {
	assert.Equal(t, "1759478400", timestampForLog("1759478400"))
	assert.Equal(t, "", timestampForLog(""))
	assert.Equal(t, "<len=33>", timestampForLog("123456789012345678901234567890123"))
	assert.Equal(t, "<unprintable len=11>", timestampForLog("12345678\n90"))
	assert.Equal(t, "2026-10-03T12:00:00Z", timestampForLog("2026-10-03T12:00:00Z"), "32 位内的可打印 ASCII 原样落")
}
