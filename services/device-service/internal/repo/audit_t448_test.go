// T448：清除设备 WiFi 的留痕复用既有审计动作词 data_modify —— 本用例钉「同源」。
//
// 为什么钉这一条：audit_logs.action 库里没有 CHECK（000001:301-313 只有列定义），
// 词表完全靠约定；约定一旦漂走，admin-web 的操作日志筛选下拉就筛不出这一行，
// 「谁在何时清了哪台设备的凭据」在页面上等于不存在。
//
// 三处 = device-service 侧写入值（本包 auditActionDataModify）、
// user-service 侧动作常量块（services/user-service/internal/handler/audit_t252.go）、
// admin-web 筛选下拉（apps/admin-web/src/pages/settings/index.vue）。
//
// 用普通 go test（不需要真库）：与 model/wifi_status_t447_test.go 同一取向，
// 没有 Docker 的作业也覆盖得到；真库那一格在 repo/audit_t448_integration_test.go。
package repo

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// 审计动作词的「上游定义处」与「消费处」，两处都在 user-service / admin-web 侧。
	userServiceAuditFile = "services/user-service/internal/handler/audit_t252.go"
	adminWebSettingsFile = "apps/admin-web/src/pages/settings/index.vue"
)

// repoRootT448 从本文件上溯 4 级到仓根（model 包 T447 同一取径）
func repoRootT448(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	dir, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
	require.NoError(t, err)
	return dir
}

// upstreamAuditActions 取出 user-service 常量块里全部 auditAction* 的字符串值。
var upstreamAuditActions = regexp.MustCompile(`(?m)^\tauditAction\w+\s*=\s*"([^"]+)"`)

func TestT448_AuditActionWordIsUpstreamVocabulary(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRootT448(t), filepath.FromSlash(userServiceAuditFile)))
	require.NoError(t, err, "user-service 审计埋点文件被移动或改名时本用例必须响：动作词失去上游定义")

	var values []string
	for _, m := range upstreamAuditActions.FindAllStringSubmatch(string(raw), -1) {
		values = append(values, m[1])
	}
	// 正对照：读取器要真能读出东西，否则「属词表内」会退化成「谁都过」。
	require.NotEmpty(t, values, "上游动作词读出为空 = 探测失效（常量块写法变了要同步改本探测）")

	assert.Contains(t, values, auditActionDataModify,
		"device-service 写的 action=%q 不在 user-service 的动作词表 %q 里：跨服务词表已分叉，审计查询页筛不到清除留痕",
		auditActionDataModify, values)

	// 反证：本卡「不加新动作词」是刻意选择。若有人改成自造词，上面那条会红；
	// 这里再钉一次「未登记的同形词确实不在词表内」，证明 Contains 不是恒真。
	for _, notIn := range []string{"wifi_clear", "clear_wifi", "config_change_x"} {
		assert.NotContains(t, values, notIn, "负样本 %q 本不该在词表里，它出现说明探测读脏了", notIn)
	}
}

func TestT448_AdminWebCanFilterChosenAction(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRootT448(t), filepath.FromSlash(adminWebSettingsFile)))
	require.NoError(t, err, "admin-web 系统配置页改名/移动时本用例必须响：筛选下拉失去看守")

	src := string(raw)
	assert.Contains(t, src, `value="`+auditActionDataModify+`"`,
		"操作日志筛选下拉里没有 %q 这一项：留痕能查但筛不出，等于没留", auditActionDataModify)

	// 反证：下拉里确实存在别的动作项，说明这一段是被读进来的、不是空文件巧合。
	assert.Contains(t, src, `value="login"`, "下拉里连 login 都读不到 = 页面结构变了，本探测要跟着改")
}

// TestT448_WifiClearTargetTypeIsDevice 钉 target_type 词形。
// devices 表在库里就叫 device 单数域；admin 查询按 target_type 过滤，
// 写成 devices 会让这一行在「按对象类型筛」时静默消失。
func TestT448_WifiClearTargetTypeIsDevice(t *testing.T) {
	assert.Equal(t, "device", auditTargetTypeDevice)
	assert.Equal(t, "data_modify", auditActionDataModify)
}
