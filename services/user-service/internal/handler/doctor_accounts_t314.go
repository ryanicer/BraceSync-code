// T314 医护账号管理·后端（PRD §7D.10「医护账号管理」·合同《功能清单》:48）
//
// 四类写端点（🔴 无 DELETE，PRD（2）「删除」按稿面无入口不入需求）：
//
//	POST /api/v1/admin/doctors                    创建：服务端发 doc+5 位序号 + 随机初始密码
//	PUT  /api/v1/admin/doctors/:doctorId          编辑：姓名/科室/团队/职称/手机号（不含密码）
//	POST /api/v1/admin/doctors/:doctorId/reset-password  重置密码：新随机密码一次性返回
//	POST /api/v1/admin/doctors/:doctorId/status    禁用/启用：body 复用 toggleRequest{action}
//
// 读侧不新建端点：GET /api/v1/doctors 已是 adminOnly，扩 admins 侧三列即覆盖本页
// 「登录账号 / 创建时间 / 状态」两表列（PRD（6）建议的 GET /admin/accounts 未建，见交件登记）。
package handler

import (
	"crypto/rand"
	"errors"
	"math/big"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// 账号状态取值（与 admins.status / doctors.status 的 CHECK 约束一致，000001:36/49）
const (
	accountStatusEnabled  = "enabled"
	accountStatusDisabled = "disabled"
)

// 字段长度上限 = DB 列宽（000001:33-34,42-44）；超长先回 400，不让运维看到 22001 的 500
const (
	doctorNameMaxLen   = 64  // admins.name / doctors.name VARCHAR(64)
	doctorTitleMaxLen  = 64  // doctors.title VARCHAR(64)
	doctorDeptMaxLen   = 128 // doctors.department VARCHAR(128)
	doctorTeamMaxLen   = 32  // doctors.team_id VARCHAR(32)
	doctorPasswordLen  = 16  // Br + 12 随机位 + #7
	doctorPwdRandomLen = 12
)

// doctorPwdAlphabet 随机密码字符集：去掉易混字形（0/O、1/I/l），因为凭据要人工口头/抄写传递。
// 大小写字母 + 数字共 57 个符号，12 位随机 ≈ 69 bit，再固定带大写/数字/符号三类。
const doctorPwdAlphabet = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

const (
	doctorPwdPrefix = "Br" // 对齐设计稿 医生账号.html:297 genPwd() 的形态
	doctorPwdSuffix = "#7"
)

// doctorAccountCreateRequest 创建入参（PRD（4）：姓名/科室/团队/职称/初始状态必填，手机号选填）
type doctorAccountCreateRequest struct {
	Name       string  `json:"name"`
	Title      string  `json:"title"`
	Department string  `json:"department"`
	TeamID     string  `json:"teamId"`
	Phone      string  `json:"phone"`
	Status     *string `json:"status"` // 缺省 = enabled
}

// doctorAccountUpdateRequest 编辑入参：指针 = 未给的字段不改（与 T302 settings 同语义）。
// Phone 指空串 = 清空手机号（选填字段要能撤回）。登录账号与密码不接受入参（PRD（4）编辑态密码分组隐藏）。
type doctorAccountUpdateRequest struct {
	Name       *string `json:"name"`
	Title      *string `json:"title"`
	Department *string `json:"department"`
	TeamID     *string `json:"teamId"`
	Phone      *string `json:"phone"`
}

// genDoctorPassword 生成一次性初始密码（crypto/rand，🔴 不用设计稿的 Math.random 演示实现）。
func genDoctorPassword() (string, error) {
	var b strings.Builder
	b.WriteString(doctorPwdPrefix)
	max := big.NewInt(int64(len(doctorPwdAlphabet)))
	for i := 0; i < doctorPwdRandomLen; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b.WriteByte(doctorPwdAlphabet[n.Int64()])
	}
	b.WriteString(doctorPwdSuffix)
	return b.String(), nil
}

// normDoctorStatus 初始状态入参 → 库内枚举；空指针取 enabled（PRD（4）默认启用）
func normDoctorStatus(p *string) (string, *model.AppError) {
	if p == nil || strings.TrimSpace(*p) == "" {
		return accountStatusEnabled, nil
	}
	switch s := strings.TrimSpace(*p); s {
	case accountStatusEnabled, accountStatusDisabled:
		return s, nil
	default:
		return "", model.ErrInvalidParam("invalid status: %q (enabled|disabled)", s)
	}
}

// checkDoctorProfileFields 姓名/职称/科室/团队四项必填 + 列宽校验。
// 返回清理过的值；职称与科室按 PRD（0）为自由文本（4 项预置可扩展、科室层级化待裁），不做枚举硬校验。
func checkDoctorProfileFields(name, title, department, teamID string) (string, string, string, string, *model.AppError) {
	name, title = strings.TrimSpace(name), strings.TrimSpace(title)
	department, teamID = strings.TrimSpace(department), strings.TrimSpace(teamID)
	for _, f := range []struct {
		label, val string
		max        int
	}{
		{"name", name, doctorNameMaxLen},
		{"title", title, doctorTitleMaxLen},
		{"department", department, doctorDeptMaxLen},
		{"teamId", teamID, doctorTeamMaxLen},
	} {
		if f.val == "" {
			return "", "", "", "", model.ErrInvalidParam("%s is required", f.label)
		}
		if len(f.val) > f.max {
			return "", "", "", "", model.ErrInvalidParam("%s too long: %d > %d", f.label, len(f.val), f.max)
		}
	}
	return name, title, department, teamID, nil
}

// doctorPhoneCipherForUpdate 编辑态手机号处理：nil 不改；空串清空；非空校验后加密+哈希。
func (h *Handler) doctorPhoneCipherForUpdate(c *gin.Context, p *string) (enc []byte, hash *string, appErr *model.AppError) {
	if p == nil {
		return nil, nil, nil
	}
	plain := strings.TrimSpace(*p)
	if plain == "" {
		return nil, strp(""), nil // 清空：enc 传 nil、hash 传空串指针，repo 落两列 NULL
	}
	if !validPhone(plain) {
		return nil, nil, model.ErrInvalidParam("invalid phone: must be 11 digits starting with 1")
	}
	enc, hashStr, appErr := h.preparePhone(plain)
	if appErr != nil {
		return nil, nil, appErr
	}
	return enc, strp(hashStr), nil
}

// strp 取地址小工具（指针语义入参用）
func strp(s string) *string { return &s }

// failDoctorAccountErr 医护账号写端点的统一错误映射（四个端点共用，保证同一档案/账号缺失语义同码）。
//
// 🔴 PRD（6）「重置密码的反馈通道」：不得复用登录侧 10401（凭据错误与账号禁用同码、
// 前端压成一条），失败原因一律走写接口自身的返回码。
func failDoctorAccountErr(c *gin.Context, err error, action string) {
	switch {
	case errors.Is(err, repo.ErrDoctorNotFound):
		fail(c, model.ErrNotFound("doctor not found: %s", c.Param("doctorId")))
	case errors.Is(err, repo.ErrDoctorNoAccount):
		// 档案在、没有可改的登录凭据（admin_id 为 NULL 的存量行）⇒ 409 而非 404
		fail(c, model.ErrConflict("%s has no login account bound, cannot %s (admin_id is null)",
			c.Param("doctorId"), action))
	case errors.Is(err, repo.ErrUsernameExhausted):
		// 发号器连续撞上已占用序号：数据异常，回 500 让运维查，不伪装成入参错误
		fail(c, model.ErrInternal("login account sequence unavailable, %s: %v", action, err))
	default:
		fail(c, model.ErrInternal("%s failed: %v", action, err))
	}
}

// createDoctorAccount POST /api/v1/admin/doctors —— 创建（同事务写 admins + doctors 两表）
func (h *Handler) createDoctorAccount(c *gin.Context) {
	var req doctorAccountCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	name, title, department, teamID, appErr := checkDoctorProfileFields(req.Name, req.Title, req.Department, req.TeamID)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	status, appErr := normDoctorStatus(req.Status)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	// 团队 FK 前置（友好 400 替代 23503 的 500），复用技师侧同一校验器
	if appErr := h.validateTechTeam(c, &teamID); appErr != nil {
		fail(c, appErr)
		return
	}

	var enc []byte
	var hash string
	if plain := strings.TrimSpace(req.Phone); plain != "" {
		if !validPhone(plain) {
			fail(c, model.ErrInvalidParam("invalid phone: must be 11 digits starting with 1"))
			return
		}
		enc, hash, appErr = h.preparePhone(plain)
		if appErr != nil {
			fail(c, appErr)
			return
		}
	}

	password, err := genDoctorPassword()
	if err != nil {
		fail(c, model.ErrInternal("generate initial password failed"))
		return
	}
	hash1, err := GenerateBcryptHash([]byte(password))
	if err != nil {
		fail(c, model.ErrInternal("hash password failed"))
		return
	}

	row, err := h.store.CreateDoctorAccount(c.Request.Context(), repo.DoctorAccountInput{
		Name: name, Title: title, Department: department, TeamID: teamID,
		PhoneEnc: enc, PhoneHash: hash, PasswordHash: hash1, Status: status,
	})
	if err != nil {
		// username 撞号已重发仍失败 = 发号器与历史数据不可用：500，不伪装成入参错误
		failDoctorAccountErr(c, err, "create doctor account")
		return
	}
	ok(c, model.DoctorAccountCreateDTO{DoctorDTO: h.toDoctorDTO(*row), InitialPassword: password})
}

// updateDoctorAccount PUT /api/v1/admin/doctors/:doctorId —— 编辑档案（不改登录账号、不改密码）
func (h *Handler) updateDoctorAccount(c *gin.Context) {
	doctorID := c.Param("doctorId")
	var req doctorAccountUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}

	in := repo.DoctorAccountUpdate{}
	if req.Name != nil || req.Title != nil || req.Department != nil || req.TeamID != nil {
		// 给了任一项档案字段就按整组必填校验（编辑模态框本来就带全 4 项，PRD（4））
		name, title, department, teamID, appErr := checkDoctorProfileFields(
			strOr(req.Name, ""), strOr(req.Title, ""), strOr(req.Department, ""), strOr(req.TeamID, ""))
		if appErr != nil {
			fail(c, appErr)
			return
		}
		if appErr := h.validateTechTeam(c, &teamID); appErr != nil {
			fail(c, appErr)
			return
		}
		in.Name, in.Title, in.Department, in.TeamID = &name, &title, &department, &teamID
	}
	enc, hash, appErr := h.doctorPhoneCipherForUpdate(c, req.Phone)
	if appErr != nil {
		fail(c, appErr)
		return
	}
	in.PhoneEnc, in.PhoneHash = enc, hash

	row, err := h.store.UpdateDoctorAccount(c.Request.Context(), doctorID, in)
	if err != nil {
		failDoctorAccountErr(c, err, "update doctor account")
		return
	}
	ok(c, h.toDoctorDTO(*row))
}

// resetDoctorAccountPassword POST /api/v1/admin/doctors/:doctorId/reset-password
// —— 重置密码：新随机密码一次性返回，旧密码即时失效（PRD（3））
func (h *Handler) resetDoctorAccountPassword(c *gin.Context) {
	doctorID := c.Param("doctorId")
	password, err := genDoctorPassword()
	if err != nil {
		fail(c, model.ErrInternal("generate password failed"))
		return
	}
	hash, err := GenerateBcryptHash([]byte(password))
	if err != nil {
		fail(c, model.ErrInternal("hash password failed"))
		return
	}
	row, err := h.store.SetDoctorAccountPassword(c.Request.Context(), doctorID, hash)
	if err != nil {
		failDoctorAccountErr(c, err, "reset password")
		return
	}
	ok(c, model.DoctorAccountResetDTO{
		DoctorID: row.DoctorID,
		Username: strOr(row.Username, ""),
		Password: password,
	})
}

// setDoctorAccountStatus POST /api/v1/admin/doctors/:doctorId/status —— 禁用/启用（幂等）
// body 复用技师侧 toggleRequest{action: enable|disable}，不另造第二种开关形状。
func (h *Handler) setDoctorAccountStatus(c *gin.Context) {
	var req toggleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, model.ErrInvalidParam("invalid request body: %v", err))
		return
	}
	var status string
	switch req.Action {
	case "enable":
		status = accountStatusEnabled
	case "disable":
		status = accountStatusDisabled
	default:
		fail(c, model.ErrInvalidParam("invalid action: %s (enable|disable)", req.Action))
		return
	}
	row, err := h.store.SetDoctorAccountStatus(c.Request.Context(), c.Param("doctorId"), status)
	if err != nil {
		failDoctorAccountErr(c, err, "toggle doctor account")
		return
	}
	ok(c, h.toDoctorDTO(*row))
}
