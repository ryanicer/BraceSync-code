//go:build integration
// +build integration

// T389-U2：真实 PG 上的医护 / 客服拒绝腿。
//
// 缺陷原貌（卡面 U2）：integration 标签下 device-service 的身份只有技师与患者两种
// （provision_ownership_integration_test.go），医护 / 客服走真库这条路一格都没有 ⇒
// 门禁只在内存 FakeStore 上绿过，不等于 PGStore 这条事务链路上没人绕过。
//
// 本文件补的就是这一格，三件事一起判：
//  1. 七条写端点 × {医护, 客服} → HTTP 403，业务码逐字 20403（字面量，理由见 N1 说明）；
//  2. 四张写表（devices / device_bindings / install_records / baselines）行数增量全 0，
//     且目标设备行、安装记录行的关键列逐字不变 —— 计数字段用 count(*)，改值型越权（UPDATE
//     不新增行）靠关键列捕获，两条合起来才是「零写」；
//  3. 反证：同样这套快照，技师发一次必须看得见行数变化 —— 否则本判据是在守一个空 detector。
//
// 运行：make test-integration（CI go-integration job 已含 ./services/device-service/...）
package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

// it389CodeForbidden 与 N1 同源：这里也写字面量，不许换成 model.CodeForbidden。
const it389CodeForbidden = 20403

const (
	it389Device   = "DEV-IT-T389"
	it389PatientA = "P-IT-T389-A" // 设备当前绑定对象
	it389PatientB = "P-IT-T389-B" // 医护若要越权会把设备改绑到它名下
	it389Tech     = "T-IT-T389"
	it389Doctor   = "ADM-D-IT-T389" // 医护账号号；门禁在取号之前就已拒绝，此值只为报文完整
	it389CS       = "CS-IT-T389"
)

// it389EnsureTech 技师行夹具：install_records.tech_id 与 baselines.calibrator_id 均指向它
func it389EnsureTech(t *testing.T) {
	t.Helper()
	_, err := itPool.Exec(context.Background(),
		`INSERT INTO technicians (tech_id, name, phone_enc, phone_hash)
		 VALUES ($1, 'T389技师', '\x00'::bytea, $2)
		 ON CONFLICT (tech_id) DO NOTHING`,
		it389Tech, "d389"+strings.Repeat("0", 60))
	require.NoError(t, err, "插入技师夹具失败")
}

// it389Counts 四张写表的行数快照
type it389Counts struct{ devices, bindings, installs, baselines int }

func it389CountTable(t *testing.T, table string) int {
	t.Helper()
	var n int
	require.NoError(t, itPool.QueryRow(context.Background(),
		fmt.Sprintf(`SELECT COUNT(*) FROM %s`, table)).Scan(&n), "count %s", table)
	return n
}

func it389Snapshot(t *testing.T) it389Counts {
	t.Helper()
	return it389Counts{
		devices:   it389CountTable(t, "devices"),
		bindings:  it389CountTable(t, "device_bindings"),
		installs:  it389CountTable(t, "install_records"),
		baselines: it389CountTable(t, "baselines"),
	}
}

// it389DeviceRow 目标设备的关键列（改值型越权的捕获面）
func it389DeviceRow(t *testing.T) string {
	t.Helper()
	var patientID, status, wifiSSID, bindTime, updatedAt string
	require.NoError(t, itPool.QueryRow(context.Background(),
		`SELECT COALESCE(patient_id,'<NULL>'), status, COALESCE(wifi_ssid,'<NULL>'),
		        COALESCE(bind_time::text,'<NULL>'), updated_at::text
		 FROM devices WHERE device_id = $1`, it389Device).
		Scan(&patientID, &status, &wifiSSID, &bindTime, &updatedAt), "读取设备行失败")
	return fmt.Sprintf("patient=%s status=%s wifi=%s bindTime=%s updatedAt=%s",
		patientID, status, wifiSSID, bindTime, updatedAt)
}

// it389InstallRow 安装记录的关键列
func it389InstallRow(t *testing.T, installID int64) string {
	t.Helper()
	var notes, sigURL, baselineID, calibTime string
	require.NoError(t, itPool.QueryRow(context.Background(),
		`SELECT COALESCE(notes,'<NULL>'), COALESCE(signature_url,'<NULL>'),
		        COALESCE(baseline_id::text,'<NULL>'), calibrate_time::text
		 FROM install_records WHERE install_id = $1`, installID).
		Scan(&notes, &sigURL, &baselineID, &calibTime), "读取安装记录失败")
	return fmt.Sprintf("notes=%s sig=%s baseline=%s calibTime=%s", notes, sigURL, baselineID, calibTime)
}

// it389Req 带身份的写请求（role 必显式给，避免命中测试夹具里的技师代发）
func it389Req(role, uid string) map[string]string {
	return map[string]string{"X-Role": role, "X-User-Id": uid}
}

func it389Offsets() []float32 {
	v := make([]float32, model.PointCount)
	for i := range v {
		v[i] = 2
	}
	return v
}

// it389Probes 卡面七条写端点的越权探针（目标一律指向「别人的资源」，即真正有害的那一组参数）
func it389Probes(installID int64) []struct {
	name, method, path string
	body               any
} {
	instPath := "/api/v1/install-records/" + strconv.FormatInt(installID, 10)
	return []struct {
		name, method, path string
		body               any
	}{
		{"bind", http.MethodPost, "/api/v1/devices/" + it389Device + "/bind",
			map[string]string{"patientId": it389PatientB}},
		{"rebind", http.MethodPost, "/api/v1/devices/" + it389Device + "/rebind",
			map[string]string{"patientId": it389PatientB}},
		{"unbind", http.MethodPost, "/api/v1/devices/" + it389Device + "/unbind", nil},
		{"wifi", http.MethodPost, "/api/v1/devices/" + it389Device + "/wifi",
			map[string]string{"ssid": "T389-EVIL"}},
		{"create-install", http.MethodPost, "/api/v1/install-records", map[string]string{
			"deviceId": it389Device, "patientId": it389PatientA, "techId": it389Tech}},
		{"update-install-meta", http.MethodPut, instPath,
			map[string]string{"notes": "T389 越权回填"}},
		{"save-baseline", http.MethodPost, "/api/v1/baselines", map[string]any{
			"installId": strconv.FormatInt(installID, 10), "offsetValues": it389Offsets()}},
	}
}

// TestIT_T389_DeviceWritesDeniedForDoctorAndCS 真库拒绝腿 + 零写 + 技师反证
func TestIT_T389_DeviceWritesDeniedForDoctorAndCS(t *testing.T) {
	itEnsurePatient(t, it389PatientA, "T389甲", "d38a")
	itEnsurePatient(t, it389PatientB, "T389乙", "d38b")
	it389EnsureTech(t)

	env := itEnv(t)

	// 技师腿造现场：注册 → 绑定 → 建安装记录（全走真实端点，不手改库）
	_, resp := env.do(t, http.MethodPost, "/api/v1/devices",
		map[string]string{"deviceId": it389Device}, nil)
	require.Equal(t, 0, resp.Code, "注册失败：%s", string(resp.Data))
	_, resp = env.do(t, http.MethodPost, "/api/v1/devices/"+it389Device+"/bind",
		map[string]string{"patientId": it389PatientA}, it389Req("technician", it389Tech))
	require.Equal(t, 0, resp.Code, "绑定失败：%s", string(resp.Data))
	_, resp = env.do(t, http.MethodPost, "/api/v1/install-records", map[string]string{
		"deviceId": it389Device, "patientId": it389PatientA, "techId": it389Tech,
	}, it389Req("technician", it389Tech))
	require.Equal(t, 0, resp.Code, "建安装记录失败：%s", string(resp.Data))

	var installID int64
	require.NoError(t, itPool.QueryRow(context.Background(),
		`SELECT install_id FROM install_records WHERE device_id = $1 ORDER BY install_id DESC LIMIT 1`,
		it389Device).Scan(&installID), "回读安装记录号失败")
	require.NotZero(t, installID)
	t.Logf("[T389-it] 现场就绪 device=%s 绑定=%s installId=%d", it389Device, it389PatientA, installID)

	// 1 + 2. 拒绝腿
	for _, role := range []string{roleDoctor, "ROLE_CS"} {
		uid := it389Doctor
		if role != roleDoctor {
			uid = it389CS
		}
		for _, p := range it389Probes(installID) {
			t.Run(p.name+"/"+role, func(t *testing.T) {
				beforeCounts := it389Snapshot(t)
				beforeDev := it389DeviceRow(t)
				beforeInst := it389InstallRow(t, installID)

				status, r := env.do(t, p.method, p.path, p.body, it389Req(role, uid))

				assert.Equal(t, http.StatusForbidden, status, "%s %s → body=%s",
					p.method, p.path, string(r.Data))
				assert.Equal(t, it389CodeForbidden, r.Code, "业务码须逐字 %d，message=%s",
					it389CodeForbidden, r.Message)
				assert.Equal(t, beforeCounts, it389Snapshot(t),
					"拒绝路径不得触碰任何一张写表（四张表行数须逐张相等）")
				assert.Equal(t, beforeDev, it389DeviceRow(t), "拒绝路径不得改写设备行")
				assert.Equal(t, beforeInst, it389InstallRow(t, installID), "拒绝路径不得改写安装记录行")
			})
		}
	}

	// 3. 反证：上面那套快照 detector 真的看得见技师的写（否则零写判据是空的）
	t.Run("counterproof/technician-write-is-visible", func(t *testing.T) {
		before := it389Snapshot(t)
		_, r := env.do(t, http.MethodPost, "/api/v1/devices/"+it389Device+"/wifi",
			map[string]string{"ssid": "T389-LEGIT"}, it389Req("technician", it389Tech))
		require.Equal(t, 0, r.Code, "技师正常写应成功：%s", r.Message)
		assert.Contains(t, it389DeviceRow(t), "wifi=T389-LEGIT", "关键列快照应显出这次改写")

		_, r = env.do(t, http.MethodPost, "/api/v1/install-records", map[string]string{
			"deviceId": it389Device, "patientId": it389PatientA, "techId": it389Tech,
		}, it389Req("technician", it389Tech))
		require.Equal(t, 0, r.Code, "技师再建一条安装记录应成功：%s", r.Message)
		assert.Equal(t, before.installs+1, it389Snapshot(t).installs,
			"反证：行数快照必须能看见一次真实写入")
	})
}
