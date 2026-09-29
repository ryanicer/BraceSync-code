// T485：安装记录写通路的审计留痕（作用对象类型 install_record）。
//
// 缺口两头（派发单 §一）：审计词表里没有「安装记录」这一类，且安装记录的两条 HTTP 主写
// （POST /install-records、PUT /install-records/:id）零留痕 —— 两者叠起来，
// 「谁在何时改了哪条安装记录」在合规面上等于不存在。
//
// 通路沿用 T448 的先例（device-service 同库直写 audit_logs，target_type/action 都进
// user-service 的词表形状），落点在 handler 而非 user-service 的 auditRoutes 表：
// 那 20 条表项是 user-service 自己的路由，跨服务塞不进中间件。
//
// 失败口径与 T448 相反、与 user-service h.audit 相同：主写已经落库，留痕写不进只记 WARN，
// 不许把它反转成 5xx —— 否则运维只能停机，风险高于漏一行留痕。
package handler

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"github.com/bracesync/bracesync/services/device-service/internal/repo"
)

// 留痕描述文案：落进 detail->>'description'（audit_logs 无 description 列），
// admin 审计页只有「操作描述」一列，不渲染 detail，故这一句必须自带对象号。
const (
	installAuditDescCreate = "新建安装记录 %s"
	installAuditDescUpdate = "回填安装记录 %s 的元数据"
)

// changedInstallMetaColumns 本次 PUT 实际提交的列名（nil = 该列 COALESCE 保持原值，不进留痕）。
// 词形取库列名，与 repo.UpdateInstallMeta 的 SET 子句一一对应，读留痕的人不必再翻两层。
func changedInstallMetaColumns(notes, signatureURL, wifiStatus *string) []string {
	var changed []string
	if notes != nil {
		changed = append(changed, "notes")
	}
	if signatureURL != nil {
		changed = append(changed, "signature_url")
	}
	if wifiStatus != nil {
		changed = append(changed, "wifi_status")
	}
	return changed
}

// auditInstallRecord 安装记录写成功后的留痕。身份取网关注入头（operatorID 是 T261 的单一来源，
// changed 是本次请求实际提交的非 nil 列名，创建路径传 nil）。
func (h *Handler) auditInstallRecord(c *gin.Context, installID int64, descFormat string, changed []string) {
	targetID := strconv.FormatInt(installID, 10)
	err := h.svc.AuditInstallRecord(c.Request.Context(), repo.InstallAuditInput{
		InstallID:    targetID,
		OperatorID:   operatorID(c),
		OperatorRole: c.GetHeader(headerRole),
		IP:           c.ClientIP(),
		Description:  fmt.Sprintf(descFormat, targetID),
		Changed:      changed,
	})
	if err != nil {
		log.Warn().Err(err).
			Str("request_id", requestIDOf(c)).
			Str("target_type", "install_record").
			Str("target_id", targetID).
			Msg("install record audit write failed")
	}
}
