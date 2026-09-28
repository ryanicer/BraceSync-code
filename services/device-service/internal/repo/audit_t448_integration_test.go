//go:build integration
// +build integration

// T448：清除设备 WiFi 的留痕在真库（PG15）里真的落得下、真的查得出。
//
// 与单测的分工（同 T447 那套三层）：
//   - repo/audit_t448_test.go 证「动作词仍属 user-service 词表、admin-web 筛得出」（无需 Docker）；
//   - handler/wifi_clear_t448_test.go 证「cleared 支只写审计、SSID 回写支行为不变、越权与 404 零写」
//     （内存桩，桩不执行列宽与 NULL/空串语义）；
//   - 本文件只证桩证不了的那一半：INSERT 语句真能在 audit_logs 上跑通、
//     空串真落成 NULL（不是空字符串，否则「未填」会被当成一个空名的操作人）、
//     且留痕这一笔不碰 devices.wifi_ssid / install_records.wifi_status 任何一列。
//
// 运行：make test-integration（需 Docker；本机无 Docker 时由 CI 跑，按用例名核日志）
package repo

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

// t448Device 本文件专用设备号（每例一台，避免按 target_id 读回时互相污染）
func t448Device(i int) string { return fmt.Sprintf("DEV-T448-IT-%d", i) }

// t448Audit 留痕行的回读形状。可空列用 pgtype.Text，这样「NULL」和「空串」分得开
// —— 混成一个形状就证不了本卡要的口径。
type t448Audit struct {
	OperatorID   pgtype.Text
	OperatorRole pgtype.Text
	Action       string
	TargetType   pgtype.Text
	TargetID     string
	IP           pgtype.Text
	Description  string
	HasTs        bool
}

// t448ReadAudit 按 target_id 读回留痕行；恰好一行，行数不对直接 require 响
func t448ReadAudit(t *testing.T, deviceID string) t448Audit {
	t.Helper()
	ctx := context.Background()

	var count int
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT count(*) FROM audit_logs WHERE target_type = 'device' AND target_id = $1`, deviceID,
	).Scan(&count))
	require.Equal(t, 1, count, "target_id=%s 的清除留痕应有且只有一行", deviceID)

	var got t448Audit
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT operator_id, operator_role, action, target_type, target_id, ip,
                COALESCE(detail->>'description', ''), ts IS NOT NULL
           FROM audit_logs WHERE target_type = 'device' AND target_id = $1`, deviceID,
	).Scan(&got.OperatorID, &got.OperatorRole, &got.Action, &got.TargetType,
		&got.TargetID, &got.IP, &got.Description, &got.HasTs))
	return got
}

// TestITT448_WifiClearAudit_LandsQueryableRow 留痕落库且能被现有筛选条件命中。
// 「谁 / 何时 / 哪台设备」三个量必须都在：operator_id、ts（列默认 now()）、target_id。
// 最后一段按 admin 查询页实际用的过滤条件筛（action + target_type），
// 证明这一行不是只写得出、筛不出。
func TestITT448_WifiClearAudit_LandsQueryableRow(t *testing.T) {
	ctx := context.Background()
	store := newITStore()
	dev := t448Device(1)
	itRegister(ctx, t, store, dev)

	require.NoError(t, store.WriteWifiClearAudit(ctx, WifiClearAuditInput{
		DeviceID: dev, OperatorID: itTech, OperatorRole: "ROLE_TECHNICIAN", IP: "10.1.2.3",
	}))

	got := t448ReadAudit(t, dev)
	assert.Equal(t, itTech, got.OperatorID.String, "operator_id：取 gateway 注入的 X-User-Id")
	assert.Equal(t, "ROLE_TECHNICIAN", got.OperatorRole.String, "operator_role")
	assert.Equal(t, auditActionDataModify, got.Action, "action 必须是 user-service 词表里的那个值")
	assert.Equal(t, auditTargetTypeDevice, got.TargetType.String, "target_type")
	assert.Equal(t, dev, got.TargetID, "target_id = 设备标识")
	assert.Equal(t, "10.1.2.3", got.IP.String, "ip")
	assert.Equal(t, wifiClearAuditDescriptor, got.Description, "detail->>'description'")
	assert.True(t, got.HasTs, "ts 由列默认值 now() 给出：留痕必须有时刻")

	var hit int
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT count(*) FROM audit_logs WHERE action = 'data_modify' AND target_type = 'device' AND target_id = $1`,
		dev).Scan(&hit))
	assert.Equal(t, 1, hit, "admin 操作日志按动作 + 对象类型筛时要能命中清除留痕")
}

// TestITT448_WifiClearAudit_EmptyOptionalBecomesNull 空串落 NULL 的口径与 user-service 一致。
// 后果：落成空串的行在「按操作人筛」时会被当成一个名叫 "" 的操作人，而落 NULL 才是「未填」。
func TestITT448_WifiClearAudit_EmptyOptionalBecomesNull(t *testing.T) {
	ctx := context.Background()
	store := newITStore()
	dev := t448Device(2)
	itRegister(ctx, t, store, dev)

	require.NoError(t, store.WriteWifiClearAudit(ctx, WifiClearAuditInput{DeviceID: dev}))

	got := t448ReadAudit(t, dev)
	assert.False(t, got.OperatorID.Valid, "缺省 operator_id 应为 NULL，不是空串")
	assert.False(t, got.OperatorRole.Valid, "缺省 operator_role 应为 NULL，不是空串")
	assert.False(t, got.IP.Valid, "缺省 ip 应为 NULL，不是空串")
	assert.Equal(t, auditActionDataModify, got.Action, "action 列 NOT NULL，仍要写进词表值")
	assert.Equal(t, dev, got.TargetID, "target_id 由路径参数给定，不该被 NULLIF 洗掉")

	var emptyNamed int
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT count(*) FROM audit_logs WHERE target_type = 'device' AND target_id = $1
           AND (operator_id = '' OR operator_role = '' OR ip = '')`, dev,
	).Scan(&emptyNamed))
	assert.Equal(t, 0, emptyNamed, "不该有任何空字符串列")
}

// TestITT448_WifiClearAudit_LeavesStateColumnsUntouched 本卡的红线：只加留痕，不动状态列。
// Boss 裁定甲案明确排除「同步云端状态」，所以清完以后 wifi_ssid 必须还是配网时那个值。
// 这条一旦变红，说明有人顺手把「清除」写成了「回写空 SSID」—— 那是本卡范围外的另一案
// （T446 §二 落点其一，与「空串不覆盖」口径正面冲突，须另行裁定）。
func TestITT448_WifiClearAudit_LeavesStateColumnsUntouched(t *testing.T) {
	ctx := context.Background()
	store := newITStore()
	dev := t448Device(3)
	itRegister(ctx, t, store, dev)

	// 配网现场：建安装记录 → 走一次真实回写，devices.wifi_ssid 与 install_records.wifi_status 都被写过
	installID, err := store.CreateInstall(ctx, &model.InstallRecord{
		DeviceID: dev, PatientID: itPatient, TechID: itTech,
		CalibrateTime: time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.NoError(t, store.SetWifiSSID(ctx, dev, "BraceHome-5G"))

	var ssidBefore, statusBefore string
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT wifi_ssid FROM devices WHERE device_id = $1`, dev).Scan(&ssidBefore))
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT wifi_status FROM install_records WHERE install_id = $1`, installID).Scan(&statusBefore))
	require.NotEmpty(t, ssidBefore, "前置现场没建起来：devices.wifi_ssid 仍是空")

	require.NoError(t, store.WriteWifiClearAudit(ctx, WifiClearAuditInput{
		DeviceID: dev, OperatorID: itTech, OperatorRole: "ROLE_TECHNICIAN",
	}))

	var ssidAfter, statusAfter string
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT wifi_ssid FROM devices WHERE device_id = $1`, dev).Scan(&ssidAfter))
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT wifi_status FROM install_records WHERE install_id = $1`, installID).Scan(&statusAfter))

	assert.Equal(t, ssidBefore, ssidAfter, "清除留痕不许改 devices.wifi_ssid（状态同步是范围外项，见 T448 卡面）")
	assert.Equal(t, statusBefore, statusAfter, "清除留痕不许改 install_records.wifi_status")

	// 留痕照落不误：两个量在同一次调用里，一个多写一个漏写都该响
	assert.Equal(t, itTech, t448ReadAudit(t, dev).OperatorID.String)
}
