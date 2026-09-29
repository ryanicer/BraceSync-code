// T485：安装记录写路径的审计留痕，并把作用对象类型 install_record 登记进词表。
//
// 缺口两头（PM 2026-09-29 裁为设计缺口，立卡收口，来源 Ella T479 验收实测）：
//  1. 词表里没有「安装记录」这一类作用对象 —— audit_logs.target_type 建表时
//     （scripts/db/migrations/000001_init_schema.up.sql:301-313）就只是 VARCHAR(32)，
//     库里没有 CHECK，取值全靠约定；
//  2. 安装记录的两条 HTTP 写通路（POST /api/v1/install-records、
//     PUT /api/v1/install-records/:id）在 device-service 里零审计调用。
//     只补 1 不补 2，这一类在审计表里仍是空集，「安装记录写无审计」那条合规读数照样无判定力。
//
// 通路沿用 T448 已开的先例：audit_logs 的 owner 是 user-service，仓内没有跨服务写审计的
// HTTP 通道，而各服务连的是同一个库 ⇒ 同库直写（见本包 audit_t448.go 的文件头）。
// 动作词沿用 user-service 词表的 data_modify，不新造动词（T399 钉过动作名维度）。
//
// 零迁移：target_type 无 CHECK，新值只是词表登记，不动表结构 ⇒ 本卡没有迁移腿。
package repo

import (
	"context"
	"encoding/json"
	"fmt"
)

// auditTargetTypeInstallRecord 安装记录的作用对象词形：单数、snake_case，
// 与同族 device（T448）/ orthosis_plan / review_record 一致。
// 写成 install_records 会让这一行在「按对象类型筛」时静默消失 —— 库里没有约束兜着，
// 词形只能靠常量与本包 audit_t485_test.go 的字面锁。
const auditTargetTypeInstallRecord = "install_record"

// InstallAuditInput 安装记录留痕入参。空字符串字段落 NULL（与 user-service WriteAuditLog 同口径）。
// InstallID 用字符串传：audit_logs.target_id 是 VARCHAR(64)，且审计页按整串比对，
// 与 user-service 那侧 c.Param("id") 的形态保持一致，别一边是数字一边是文本。
// Changed 是本次请求真正送上来的列名（notes / signature_url / wifi_status），
// 供「这次改了什么」反查；创建路径留空。
type InstallAuditInput struct {
	InstallID    string
	OperatorID   string
	OperatorRole string
	IP           string
	Description  string
	Changed      []string
}

// WriteInstallRecordAudit 写一条「谁在何时改了哪条安装记录」。
//
// 与 WriteWifiClearAudit 的差别在失败处理：清除那条通路的全部产出就是审计行，失败必须回错；
// 这里的审计跟在主写（install_records）之后，安装记录已经落库，
// 所以由调用方（handler）记 WARN 而不把请求反转成 500 —— 口径同 user-service 的 h.audit。
func (r *PGStore) WriteInstallRecordAudit(ctx context.Context, in InstallAuditInput) error {
	detail := map[string]any{"description": in.Description}
	if len(in.Changed) > 0 {
		detail["changed"] = in.Changed
	}
	payload, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("marshal install record audit detail: %w", err)
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO audit_logs (operator_id, operator_role, action, target_type, target_id, detail, ip)
		 VALUES (NULLIF($1, ''), NULLIF($2, ''), $3, NULLIF($4, ''), NULLIF($5, ''), $6::jsonb, NULLIF($7, ''))`,
		in.OperatorID, in.OperatorRole, auditActionDataModify,
		auditTargetTypeInstallRecord, in.InstallID, string(payload), in.IP)
	if err != nil {
		return fmt.Errorf("write install record audit: %w", err)
	}
	return nil
}
