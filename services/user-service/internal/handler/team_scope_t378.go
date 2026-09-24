// T378：患者沟通（feedbacks）域读写归属判定在服务端落地。
//
// T350（#205）收口的四条读链路里没有 feedbacks，T373（#208）的三处里也没有：
// 于是「一条 UPDATE 打任意 feedbackId」和「列表/统计条按全院数」这两件事在医护身份下照旧成立。
// 后者比前者更重 —— T374 之后那次 UPDATE 能把状态落成 resolved，而全仓没有反向接口，即不可回滚。
//
// 本文件不重新推导范围：身份 → 团队的推导复用 #205 的 resolveTeamScope（含 fail-closed），
// 三条链路（写 1 + 读 2）只是把同一个 teamScope 换算成各自的入参。
package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// feedbackReadScope 把 #205 的 teamScope 换算成反馈读侧范围。
//
// 只收紧不放宽：不受限角色（运营 / 客服）得到 TeamScoped=false，repo 侧不加团队谓词，
// 响应逐字不变；受限角色即使 teamID 为空也照实传下去（TeamScoped=true + 空 TeamID
// = 恒假谓词 → 空集 / 全零），不得退化成「不过滤」。
func feedbackReadScope(scope teamScope) repo.FeedbackScope {
	if !scope.limited {
		return repo.FeedbackScope{}
	}
	return repo.FeedbackScope{TeamScoped: true, TeamID: scope.teamID}
}

// assertFeedbackInScope 反馈写入口（POST /feedbacks/:feedbackId/process）的门禁。
//
// 判定必须发生在 ProcessFeedback 之前：卡面判据是「越权被拒时库内变更次数为 0」，
// 把团队谓词写进那条 UPDATE 虽然也挡得住数据，但那条语句本身就是一次写（口径同 #208）。
//
// 受限身份下探测无行 = 「反馈不存在 / 患者属他团队 / 患者未分配团队 / 本人无团队归属」
// 四格合一律 403 ⇒ feedbackId 存在性不可辨（口径同 getPatient 与 #208 的 A1）。
// 不受限角色原封不动不探测：它们仍走 ProcessFeedback 自己的「查无 → 404」，
// 这一档的入参与状态码不许变。
//
// 返回 false 时响应已写出，调用方必须直接 return。
func (h *Handler) assertFeedbackInScope(c *gin.Context, feedbackID int64) bool {
	scope, allowed := h.resolveTeamScope(c)
	if !allowed {
		return false
	}
	if !scope.limited {
		return true
	}
	inTeam, err := h.store.FeedbackInTeam(c.Request.Context(), feedbackID, scope.teamID)
	if err != nil {
		fail(c, model.ErrInternal("resolve feedback scope failed"))
		return false
	}
	if !inTeam {
		fail(c, model.ErrForbidden("feedback %d is out of your data scope", feedbackID))
		return false
	}
	return true
}
