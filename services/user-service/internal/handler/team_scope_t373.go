// T373：患者域读写归属判定在服务端落地（写侧 D1/D2 + 读侧 A1）。
//
// T350（#205）把三条读链路收口成「列表按团队过滤 + 详情按团队读」，那只覆盖了读侧的一半：
// 两条写链路（医生回复佩戴感受日志 D1、保存矫形方案 D2）与「方案历史」这条读链路（A1）
// 从不判归属，任意医护令牌可改/可看任意患者的数据。
//
// 本文件不重新推导范围，只把 #205 的身份推导（resolveTeamScope）接到这三处入口，
// 且判定必须发生在写调用之前：判据要求「跨团队时库写次数为 0」，
// 把团队谓词写进 UPDATE / INSERT 虽然也挡得住数据，但那条语句本身就是一次写。
package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
)

// assertPatientInScope 患者维度的入口门禁（D2 写 / A1 读共用）。
//
// 受限身份（ROLE_DOCTOR）走 repo.GetPatientInTeam：无行可能是「患者不存在」「在他团队」
// 「患者未分配团队」「本人无团队归属」四格之一，一律同一个 403 ⇒ 患者号存在性不可辨
// （口径同 getPatient，T350 判据③ 后半）。
// 不受限角色（运营 / 客服）原封不动走 assertPatientExists：只判存在性，查无此人仍 404，
// 这一档的入参与状态码不许变（只收紧不放宽）。
//
// 返回 false 时响应已写出，调用方必须直接 return。
func (h *Handler) assertPatientInScope(c *gin.Context, patientID string) bool {
	scope, allowed := h.resolveTeamScope(c)
	if !allowed {
		return false
	}
	if !scope.limited {
		return h.assertPatientExists(c, patientID)
	}
	row, err := h.store.GetPatientInTeam(c.Request.Context(), patientID, scope.teamID)
	if err != nil {
		fail(c, model.ErrInternal("get patient failed"))
		return false
	}
	if row == nil {
		denyCrossTeam(c, patientID)
		return false
	}
	return true
}
