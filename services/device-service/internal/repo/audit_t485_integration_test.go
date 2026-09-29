//go:build integration
// +build integration

// T485：安装记录留痕在真库（PG15）里真的落得下、真的筛得出。
//
// 与单测的分工（同 T447/T448 那套三层）：
//   - repo/audit_t485_test.go 证「词形是 install_record 单数、且登记在 user-service 词表里」（无需 Docker）；
//   - handler/audit_t485_test.go 证「两条写通路各留一行、被拒与查无零留痕、留痕失败不反转主写」
//     （内存桩，桩不执行列宽与 NULL/空串语义，也不落 jsonb）；
//   - 本文件只证桩证不了的那一半：INSERT 真能在 audit_logs 上跑通、空串真落成 NULL、
//     changed 真以 JSON 数组进 detail、且这一笔留痕不碰 install_records 任何一列。
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

// t485Device 本文件专用设备号（每例一台，避免按 target_id 读回时互相污染）
func t485Device(i int) string { return fmt.Sprintf("DEV-T485-IT-%d", i) }

// t485Row 留痕行的回读形状。可空列用 pgtype.Text，这样「NULL」和「空串」分得开。
type t485Row struct {
	OperatorID   pgtype.Text
	OperatorRole pgtype.Text
	Action       string
	TargetType   pgtype.Text
	TargetID     string
	IP           pgtype.Text
	Description  string
	Changed      pgtype.Text // detail->>'changed'：整体是数组的 JSON 文本，缺键时为 NULL
	HasTs        bool
}

// t485Read 按 target_id 读回留痕行；恰好一行，行数不对直接 require 响
func t485Read(t *testing.T, installID string) t485Row {
	t.Helper()
	ctx := context.Background()

	var count int
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT count(*) FROM audit_logs WHERE target_type = 'install_record' AND target_id = $1`, installID,
	).Scan(&count))
	require.Equal(t, 1, count, "target_id=%s 的安装记录留痕应有且只有一行", installID)

	var got t485Row
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT operator_id, operator_role, action, target_type, target_id, ip,
                COALESCE(detail->>'description', ''), (detail->'changed')::text, ts IS NOT NULL
           FROM audit_logs WHERE target_type = 'install_record' AND target_id = $1`, installID,
	).Scan(&got.OperatorID, &got.OperatorRole, &got.Action, &got.TargetType,
		&got.TargetID, &got.IP, &got.Description, &got.Changed, &got.HasTs))
	return got
}

// TestITT485_InstallAudit_LandsQueryableRow 留痕落库且能被现有筛选条件命中。
// 「谁 / 何时 / 哪条安装记录」三个量必须都在：operator_id、ts（列默认 now()）、target_id。
// 末段按 admin 查询页实际用的过滤条件筛（action + target_type + target_id）。
func TestITT485_InstallAudit_LandsQueryableRow(t *testing.T) {
	ctx := context.Background()
	store := newITStore()
	dev := t485Device(1)
	itRegister(ctx, t, store, dev)

	rec, err := store.CreateInstall(ctx, &model.InstallRecord{
		DeviceID: dev, PatientID: itPatient, TechID: itTech,
		CalibrateTime: time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	installID := fmt.Sprint(rec)

	require.NoError(t, store.WriteInstallRecordAudit(ctx, InstallAuditInput{
		InstallID: installID, OperatorID: itTech, OperatorRole: "ROLE_TECHNICIAN",
		IP: "10.1.2.3", Description: "新建安装记录 " + installID,
	}))

	got := t485Read(t, installID)
	assert.Equal(t, itTech, got.OperatorID.String, "operator_id：取 gateway 注入的 X-User-Id")
	assert.Equal(t, "ROLE_TECHNICIAN", got.OperatorRole.String, "operator_role")
	assert.Equal(t, auditActionDataModify, got.Action, "action 必须是 user-service 词表里的那个值")
	assert.Equal(t, auditTargetTypeInstallRecord, got.TargetType.String, "target_type")
	assert.Equal(t, installID, got.TargetID, "target_id = 安装记录号（字符串形态）")
	assert.Equal(t, "10.1.2.3", got.IP.String, "ip")
	assert.Equal(t, "新建安装记录 "+installID, got.Description, "detail->>'description'")
	assert.False(t, got.Changed.Valid, "创建路径没有提交列清单，detail 里不该凭空有个 changed 键")
	assert.True(t, got.HasTs, "ts 由列默认值 now() 给出：留痕必须有时刻")

	var hit int
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT count(*) FROM audit_logs
		  WHERE action = 'data_modify' AND target_type = 'install_record' AND target_id = $1`,
		installID).Scan(&hit))
	assert.Equal(t, 1, hit, "admin 操作日志按动作 + 对象类型筛时要能命中安装记录留痕")
}

// TestITT485_InstallAudit_ChangedColumnsAsJSONArray 回填路径的列清单真以 JSON 数组进 detail。
// 落不成数组（比如被写成 "notes,signature_url" 这种串）时，按 jsonb 展开的那一侧读不出来。
func TestITT485_InstallAudit_ChangedColumnsAsJSONArray(t *testing.T) {
	ctx := context.Background()
	store := newITStore()
	dev := t485Device(2)
	itRegister(ctx, t, store, dev)

	id, err := store.CreateInstall(ctx, &model.InstallRecord{
		DeviceID: dev, PatientID: itPatient, TechID: itTech,
		CalibrateTime: time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	installID := fmt.Sprint(id)

	require.NoError(t, store.WriteInstallRecordAudit(ctx, InstallAuditInput{
		InstallID: installID, OperatorID: itTech, OperatorRole: "ROLE_TECHNICIAN",
		Description: "回填安装记录 " + installID + " 的元数据",
		Changed:     []string{"notes", "wifi_status"},
	}))

	got := t485Read(t, installID)
	assert.Equal(t, `["notes", "wifi_status"]`, got.Changed.String,
		"detail->>'changed' 应是 JSON 数组文本（键序按写入序）")

	// 按数组元素逐个核，别只比整串（jsonb 会重排空格，整串比对不稳）。
	var n int
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT jsonb_array_length(detail->'changed') FROM audit_logs
		  WHERE target_type = 'install_record' AND target_id = $1`, installID).Scan(&n))
	assert.Equal(t, 2, n, "changed 数组应有两个列名")

	var notesOnly int
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT count(*) FROM audit_logs WHERE target_type = 'install_record'
           AND detail->'changed' @> '["notes"]'::jsonb`).Scan(&notesOnly))
	assert.GreaterOrEqual(t, notesOnly, 1, "按「本次改了 notes 这一列」筛时必须命中")
}

// TestITT485_InstallAudit_EmptyOptionalBecomesNull 空串落 NULL 的口径与 user-service 一致。
// 后果：落成空串的行在「按操作人筛」时会被当成一个名叫 "" 的操作人，而落 NULL 才是「未填」。
func TestITT485_InstallAudit_EmptyOptionalBecomesNull(t *testing.T) {
	ctx := context.Background()
	store := newITStore()
	dev := t485Device(3)
	itRegister(ctx, t, store, dev)

	id, err := store.CreateInstall(ctx, &model.InstallRecord{
		DeviceID: dev, PatientID: itPatient, TechID: itTech,
		CalibrateTime: time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	installID := fmt.Sprint(id)

	require.NoError(t, store.WriteInstallRecordAudit(ctx, InstallAuditInput{InstallID: installID}))

	got := t485Read(t, installID)
	assert.False(t, got.OperatorID.Valid, "缺省 operator_id 应为 NULL，不是空串")
	assert.False(t, got.OperatorRole.Valid, "缺省 operator_role 应为 NULL，不是空串")
	assert.False(t, got.IP.Valid, "缺省 ip 应为 NULL，不是空串")
	assert.Equal(t, auditActionDataModify, got.Action, "action 列 NOT NULL，仍要写进词表值")
	assert.Equal(t, installID, got.TargetID, "target_id 由调用方给定，不该被 NULLIF 洗掉")

	var emptyNamed int
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT count(*) FROM audit_logs WHERE target_type = 'install_record' AND target_id = $1
           AND (operator_id = '' OR operator_role = '' OR ip = '')`, installID,
	).Scan(&emptyNamed))
	assert.Equal(t, 0, emptyNamed, "不该有任何空字符串列")
}

// TestITT485_InstallAudit_LeavesInstallColumnsUntouched 本卡的红线：只加留痕，不动 install_records。
// 留痕写发生在主写之后，若它顺带 UPDATE 了业务列（例如把 notes 洗成空），
// 「审计只是旁观者」这个前提就没了，读侧会看到被留痕改过的数据。
func TestITT485_InstallAudit_LeavesInstallColumnsUntouched(t *testing.T) {
	ctx := context.Background()
	store := newITStore()
	dev := t485Device(4)
	itRegister(ctx, t, store, dev)

	id, err := store.CreateInstall(ctx, &model.InstallRecord{
		DeviceID: dev, PatientID: itPatient, TechID: itTech,
		CalibrateTime: time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.NoError(t, store.UpdateInstallMeta(ctx, id, strP("腰托位已调"), nil, strP("connected")))

	var notesBefore, statusBefore, sigBefore string
	var sigNull bool
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT COALESCE(notes, ''), COALESCE(signature_url, ''), wifi_status
           FROM install_records WHERE install_id = $1`, id,
	).Scan(&notesBefore, &sigBefore, &statusBefore))
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT signature_url IS NULL FROM install_records WHERE install_id = $1`, id,
	).Scan(&sigNull))
	require.Equal(t, "connected", statusBefore, "前置现场没建起来：wifi_status 不是 connected")

	require.NoError(t, store.WriteInstallRecordAudit(ctx, InstallAuditInput{
		InstallID: fmt.Sprint(id), OperatorID: itTech, OperatorRole: "ROLE_TECHNICIAN",
		Description: "回填安装记录留痕", Changed: []string{"notes", "wifi_status"},
	}))

	var notesAfter, statusAfter, sigAfter string
	require.NoError(t, itPool.QueryRow(ctx,
		`SELECT COALESCE(notes, ''), COALESCE(signature_url, ''), wifi_status
           FROM install_records WHERE install_id = $1`, id,
	).Scan(&notesAfter, &sigAfter, &statusAfter))

	assert.Equal(t, notesBefore, notesAfter, "留痕不许改 install_records.notes")
	assert.Equal(t, statusBefore, statusAfter, "留痕不许改 install_records.wifi_status")
	assert.Equal(t, sigBefore, sigAfter, "留痕不许改 install_records.signature_url")
	assert.True(t, sigNull, "本例的 signature_url 本就为 NULL（没提交过），留痕不得替它编值")

	// 留痕照落不误：两个量在同一次调用里，一个多写一个漏写都该响
	assert.Equal(t, itTech, t485Read(t, fmt.Sprint(id)).OperatorID.String)
}

func strP(s string) *string { return &s }
