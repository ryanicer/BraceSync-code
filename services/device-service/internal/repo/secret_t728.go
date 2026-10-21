// T728 设备验签密钥的「回显 / 下载 / 重新生成」通路在数据层的两格：整写密钥列 + 留痕。
//
// 背景（卡面）：Boss 经 staging admin-web 注册设备后拿不到 secret，只能走 DB/API 导出经 andy
// 中转，固件烧录阻塞数小时。本卡把取密钥的通路收在服务端专用端点上，而「取过」这件事必须
// 在 audit_logs 里留痕 —— 本文件是那两格的落库实现。
//
// 零迁移：devices.secret_version 是 INT NOT NULL DEFAULT 1（000001_init_schema.up.sql:96），
// 没有 CHECK 上界 ⇒ 自增不动表结构；audit_logs 的 action / target_type 两列同样无 CHECK
// （000001:301-313），取值沿用 user-service 词表（见下面两条 action 常量的出处），不新造动词。
//
// 通路沿用 T448 / T485 的先例：audit_logs 的 owner 是 user-service，仓内没有跨服务写审计的
// HTTP 通道，而各服务连的是同一个库 ⇒ 同库直写。
package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// auditActionSensitiveRead 「按需解密读了一个高敏感值」的动作词，与 user-service 词表同值
// （services/user-service/internal/handler/audit_t252.go 的 auditActionSensitiveRead，T691 r134
// 为「查看患者手机号明文」新增的读类 action，比常规 data_read 更高敏感级）。
// 两处必须一起改；改一侧而另一侧不动，审计页按动作筛选时这一类会静默消失（库里没有 CHECK 兜着）。
const auditActionSensitiveRead = "sensitive_read"

// 留痕的作用对象词形：沿用 T448 的 device（单数、snake_case），不新造 device_secret ——
// admin 审计页的「按对象类型筛」靠的是这一格，词形漂了就筛不到。
const (
	auditTargetTypeDeviceSecret = "device"
	// maxSecretAuditTargetID audit_logs.target_id 是 VARCHAR(64)（000001:307）。
	// 单台下载填设备号；批量下载填不了（50 个号远超 64 字节）⇒ 留 NULL，
	// 完整名单在 detail->>'deviceIds' 里（口径同 user-service 的 batch-bind：target_id 空、detail 带全量）。
	maxSecretAuditTargetID = 64
)

// UpdateDeviceSecret 重新生成密钥的落库写：整写 device_secret_enc 并把 secret_version 自增。
//
// expectVersion 是调用方读到的当前版本（乐观锁）： devices 行的读与写之间可能被并发的第二次
// 重生成插队，那时本语句命中 0 行 ⇒ ErrConflict，而不是把别人的新版本覆盖掉后各自以为成功
// ——「旧 secret 即时失效」的判据靠版本号单调推进，不能被并发写抹平。
//
// 0 行的另一种解释是「设备行已被删」，本函数不区分（与 SetWifiSSID/SetContactArea 同族：
// 不把设备号存在性做成可探测面）；service 层先前已 GetDevice，故实际只剩并发这一支。
func (r *PGStore) UpdateDeviceSecret(ctx context.Context, deviceID string, secretEnc []byte, expectVersion int) (int, error) {
	var next int
	err := r.pool.QueryRow(ctx,
		`UPDATE devices
		    SET device_secret_enc = $2, secret_version = secret_version + 1, updated_at = now()
		  WHERE device_id = $1 AND secret_version = $3
		  RETURNING secret_version`,
		deviceID, secretEnc, expectVersion,
	).Scan(&next)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrConflict
	}
	if err != nil {
		return 0, fmt.Errorf("update device secret: %w", err)
	}
	return next, nil
}

// DeviceSecretReadAuditInput 下载留痕入参。单台一条、批量一条（批量只落一行，名单进 detail）。
// 空字符串字段落 NULL（与 user-service WriteAuditLog 同口径）。
//
// 🔴 DeviceIDs / Skipped 只装设备号：secret 明文一个字都不进 detail（PM 红线 3）。
// 这一条由 secret_t728_test.go 的字面锁钉住——往 detail 里多塞一个键，用例必须当场红。
type DeviceSecretReadAuditInput struct {
	DeviceIDs    []string
	Skipped      []string
	OperatorID   string
	OperatorRole string
	IP           string
	Description  string
}

// DeviceSecretRegenerateAuditInput 重生成留痕入参。Old/New 两版号进 detail，
// 让「哪一次重生成把哪一版作废了」可反查（卡面：重新生成=旧 secret 失效）。
type DeviceSecretRegenerateAuditInput struct {
	DeviceID     string
	OperatorID   string
	OperatorRole string
	IP           string
	OldVersion   int
	NewVersion   int
	Description  string
}

// WriteDeviceSecretReadAudit 写一条「谁在何时下载了哪几台设备的验签密钥」。
//
// 动作取 sensitive_read 而非 data_read：这一读拿到的是可伪造设备上报的 HMAC 密钥，
// 与「查看患者手机号明文」同级（T691 那条已把这一档动作立在词表里）。
// 失败必须回错：调用方（handler）据此**拒绝发包**，不留「取到了密钥却没留痕」的缝
// ——这与 T485 那条「主写已落库所以只 WARN」的语义相反，因为这里留痕是披露的前置条件。
func (r *PGStore) WriteDeviceSecretReadAudit(ctx context.Context, in DeviceSecretReadAuditInput) error {
	detail := map[string]any{"description": in.Description, "deviceIds": in.DeviceIDs}
	if len(in.Skipped) > 0 {
		detail["skippedDeviceIds"] = in.Skipped
	}
	payload, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("marshal device secret read audit detail: %w", err)
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO audit_logs (operator_id, operator_role, action, target_type, target_id, detail, ip)
		 VALUES (NULLIF($1, ''), NULLIF($2, ''), $3, NULLIF($4, ''), NULLIF($5, ''), $6::jsonb, NULLIF($7, ''))`,
		in.OperatorID, in.OperatorRole, auditActionSensitiveRead, auditTargetTypeDeviceSecret,
		secretAuditTargetID(in.DeviceIDs), string(payload), in.IP)
	if err != nil {
		return fmt.Errorf("write device secret read audit: %w", err)
	}
	return nil
}

// WriteDeviceSecretRegenerateAudit 写一条「谁在何时重生了哪台设备的验签密钥」。
// 动作取 data_modify（改了 devices 行的密钥列），沿用 T448/T485 那条同族动词。
func (r *PGStore) WriteDeviceSecretRegenerateAudit(ctx context.Context, in DeviceSecretRegenerateAuditInput) error {
	payload, err := json.Marshal(map[string]any{
		"description": in.Description,
		"deviceId":    in.DeviceID,
		"oldVersion":  in.OldVersion,
		"newVersion":  in.NewVersion,
	})
	if err != nil {
		return fmt.Errorf("marshal device secret regenerate audit detail: %w", err)
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO audit_logs (operator_id, operator_role, action, target_type, target_id, detail, ip)
		 VALUES (NULLIF($1, ''), NULLIF($2, ''), $3, NULLIF($4, ''), $5, $6::jsonb, NULLIF($7, ''))`,
		in.OperatorID, in.OperatorRole, auditActionDataModify, auditTargetTypeDeviceSecret,
		in.DeviceID, string(payload), in.IP)
	if err != nil {
		return fmt.Errorf("write device secret regenerate audit: %w", err)
	}
	return nil
}

// secretAuditTargetID 单台下载填那台设备号；批量（或空）填 NULL。
// 截断不做（宁可 NULL 也不留一个被砍断的假设备号）：设备号本身 ≤48 字节（VARCHAR(48)），
// 只有「多台」这一种情形会超 maxSecretAuditTargetID。
func secretAuditTargetID(deviceIDs []string) string {
	if len(deviceIDs) != 1 {
		return ""
	}
	if len(deviceIDs[0]) > maxSecretAuditTargetID {
		return ""
	}
	return deviceIDs[0]
}
