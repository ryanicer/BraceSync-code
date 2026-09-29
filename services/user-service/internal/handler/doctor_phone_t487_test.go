// Package handler 单测 T487-④：医护账号手机号撞号（23505 → 10409）的错误映射
//
// 卡面判据「唯一性 23505 友好提示」在后端这一跳只做一件事：把 repo 的
// ErrAdminPhoneTaken 折成写接口自身的 409/10409。三个不能错的边界：
//  1. 不许回 500 —— 撞号是入参问题，不是服务异常，回 500 运维查不到是哪一个号；
//  2. 不许回登录侧的 10401 —— PRD（6）已就「重置密码反馈通道」定过同一条口径
//     （凭据错误与账号禁用同码、前端压成一条），本卡的手机号冲突同理；
//  3. 不许把别的库错也折成 10409 —— 反证格：普通 db 错误必须仍是 500/90001，
//     否则「手机号被占用」会变成任何写失败的替罪文案，前端那句点名提示就成了误导。
//
// 撞号本身（uk_admins_phone_hash）在真库里证，见 repo/admin_phone_t487_it_test.go。
package handler

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

func TestT487_CreateDoctorAccount_PhoneTakenMapsTo409(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.teamExists = true
	e.store.docAcct.createErr = repo.ErrAdminPhoneTaken

	w, resp := e.do(http.MethodPost, "/api/v1/admin/doctors", t314Body(nil), t314AdminHdr())
	require.Equal(t, http.StatusConflict, w.Code, "body=%s", resp.Message)
	assert.Equal(t, model.CodeConflict, resp.Code, "撞号走写接口自身的 10409（前端据此点名手机号）")
	assert.NotEqual(t, model.CodeUnauthorized, resp.Code, "🔴 不得复用登录侧 10401（凭据错误与冲突会被压成一条）")
	assert.NotEqual(t, model.CodeInternal, resp.Code, "🔴 撞唯一键是入参问题，不是服务异常")
	// 技术日志留英文原句给运维，用户侧只拿码表中文（T464 双通道）
	t464TechLogContains(t, w, "phone already used")
}

func TestT487_UpdateDoctorAccount_PhoneTakenMapsTo409(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.teamExists = true
	e.store.docAcct.updateErr = repo.ErrAdminPhoneTaken

	w, resp := e.do(http.MethodPut, "/api/v1/admin/doctors/"+t314Doctor,
		map[string]any{"phone": "13800000002"}, t314AdminHdr())
	require.Equal(t, http.StatusConflict, w.Code, "body=%s", resp.Message)
	assert.Equal(t, model.CodeConflict, resp.Code)
	t464TechLogContains(t, w, "phone already used")
}

// TestT487_DoctorAccount_OtherDbErrorStays500 反证：不是撞号的库错不许被折成 10409。
// 若这条塌了，前端「该手机号已被其他后台账号占用」就会在连接池故障时照样弹出来。
func TestT487_DoctorAccount_OtherDbErrorStays500(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.teamExists = true
	e.store.docAcct.createErr = errors.New("connection reset by peer")

	w, resp := e.do(http.MethodPost, "/api/v1/admin/doctors", t314Body(nil), t314AdminHdr())
	require.Equal(t, http.StatusInternalServerError, w.Code, "body=%s", resp.Message)
	assert.Equal(t, model.CodeInternal, resp.Code)
	assert.NotEqual(t, model.CodeConflict, resp.Code)
}

// TestT487_AdminSideHasNoPhoneChannel 「管理员侧不加手机号录入」的机读锁（Boss 18:42 收窄第 1 条）。
//
// 这条不靠「页面上没有输入框」来保证，靠的是全仓只有一处写 admins 手机号：
// 写凭据副本的两个入口都在医护账号通道里，且 role_id 由常量固定为 ROLE_DOCTOR
// ⇒ 管理员账号（ops_admin 等 seed 通道）拿不到手机号，也就永远不会出现在手机号登录的命中集里。
// 将来谁加第三条通道（例如管理员账号 CRUD 顺手带手机号），这里先红，逼作者回到本卡口径确认。
//
// 🔴 扫描面是整个 services/ 而不是本包：别的服务将来接进同一个库时也要被这道锁看到。
func TestT487_AdminSideHasNoPhoneChannel(t *testing.T) {
	counts := countInServicesGoSources(t, "INSERT INTO admins", "UPDATE admins SET phone_enc")
	assert.Equal(t, 1, counts["INSERT INTO admins"],
		"写 admins 的入口应只有创建医护账号一处，实得 %+v", counts)
	assert.Equal(t, 1, counts["UPDATE admins SET phone_enc"],
		"改 admins 手机号的入口应只有编辑医护账号一处，实得 %+v", counts)

	// 角色固定：创建入口不接受 role 入参，只写这一个常量
	doctorSrc := readServicesSource(t,
		"user-service/internal/repo/doctor_accounts_t314.go")
	assert.Contains(t, doctorSrc, `const doctorAccountRole = "ROLE_DOCTOR"`)
	assert.Contains(t, doctorSrc, "doctorAccountRole, in.Status",
		"INSERT 用的必须是这个常量，换成入参就等于开放了角色通道")
}

// servicesRoot 从本文件位置回溯到 services/（handler 包上三级）。
// 用 runtime.Caller 而不是相对路径猜：go test 的工作目录随 -C / ./... 变，
// 但 Caller 给出的源文件路径在编译期就钉死了。
func servicesRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "取不到本测试文件路径")
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

// countInServicesGoSources 统计每个模式串在 services/**（不含 _test.go）里出现的总次数。
// 排除测试文件：本卡自己的用例里也写着这些字面量，算进去就恒不相等了。
func countInServicesGoSources(t *testing.T, patterns ...string) map[string]int {
	t.Helper()
	root := servicesRoot(t)
	counts := make(map[string]int, len(patterns))
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, p := range patterns {
			counts[p] += strings.Count(string(src), p)
		}
		return nil
	})
	require.NoError(t, err, "扫描 %s 失败", root)
	return counts
}

func readServicesSource(t *testing.T, rel string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(servicesRoot(t), filepath.FromSlash(rel)))
	require.NoError(t, err, "源文件 %s 读不到（改名或挪走了？同步这道锁）", rel)
	return string(src)
}
