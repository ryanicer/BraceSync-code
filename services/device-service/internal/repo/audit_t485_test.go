// T485：作用对象类型 install_record 的词形与「登记在上游词表里」这两件事。
//
// 为什么钉：audit_logs.target_type 建表时（scripts/db/migrations/000001_init_schema.up.sql:301-313）
// 只是 VARCHAR(32)，库里没有 CHECK，取值全靠约定 ⇒ 一旦漂成表名那种复数形（install_records），
// 这一行在「按对象类型筛」时静默消失，而没有任何约束会响。
//
// 表名与对象词形不是一回事：device 域同一套口径（devices 表 / target_type='device'，T448 已钉）。
//
// 运行：普通 go test（不需要真库），与 audit_t448_test.go 同一取向；
// 真库那一格在 repo/audit_t485_integration_test.go。
package repo

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// userServiceAuditRepoFile 审计表的 owner 侧：AuditInput.TargetType 的词表登记处。
const userServiceAuditRepoFile = "services/user-service/internal/repo/audit.go"

// repoRootT485 从本文件上溯 4 级到仓根（与 audit_t448_test.go 同一取径）
func repoRootT485(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	dir, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
	require.NoError(t, err)
	return dir
}

// TestT485_InstallTargetTypeWordShape 词形本身：单数、snake_case，不是表名。
func TestT485_InstallTargetTypeWordShape(t *testing.T) {
	assert.Equal(t, "install_record", auditTargetTypeInstallRecord,
		"作用对象词形变了要同步改上游词表登记与留痕消费方")
	assert.NotEqual(t, "install_records", auditTargetTypeInstallRecord,
		"target_type 不许写成表名：库里没有 CHECK 兜着，写复数就等于这一行筛不出来")
	// 动作维度沿用 user-service 词表，本卡不新造动词（T399 钉过动作名维度）。
	assert.Equal(t, "data_modify", auditActionDataModify)
}

// TestT485_TargetTypeIsRegisteredInUpstreamVocabulary 新类型必须登记在审计表 owner 侧的词表里。
// 读的是 user-service 的 AuditInput.TargetType 注释 —— 全仓唯一一处「作用对象类型清单」，
// 只在 device-service 侧写常量而不登记，下一个补型的人照样看不见这一类。
func TestT485_TargetTypeIsRegisteredInUpstreamVocabulary(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRootT485(t), filepath.FromSlash(userServiceAuditRepoFile)))
	require.NoError(t, err, "user-service 审计仓储文件被移动或改名时本用例必须响：词表登记处失去看守")

	src := string(raw)
	singular := regexp.MustCompile(`\binstall_record\b`)
	plural := regexp.MustCompile(`\binstall_records\b`)

	assert.True(t, singular.MatchString(src),
		"上游词表里没有 %q 这一类作用对象：审计表 owner 侧看不见它，跨服务词表已分叉", auditTargetTypeInstallRecord)
	assert.False(t, plural.MatchString(src),
		"上游词表里出现复数形 install_records：与本卡词形（单数）冲突，两处必须收口成一个")

	// 正对照：读取器真能读出这一行清单，否则「没有」会被读成「都有」。
	for _, known := range []string{"patient", "technician", "sys_config", "device"} {
		assert.Regexp(t, regexp.MustCompile(`\b`+known+`\b`), src,
			"词表里连 %q 都读不到 = 这一段没被读进来，上面的判定失效", known)
	}
	// 反证：一个本不该登记的类型确实读不到，证明上面不是恒真。
	assert.NotRegexp(t, regexp.MustCompile(`\borthosis_planx\b`), src)
}

// TestT485_UpstreamVocabularyIsTheWholeWrittenSet 词表登记处必须与「代码实际写入的作用对象类型」
// 全集相等。
//
// 为什么钉这一格：本卡的缺口是「词表看不见新类型」（派发单 §一：现网读数里压根没有安装记录这一类），
// 而登记处只是一行注释 —— 补一个类型容易，下一个类型照样会漏。这里把注释与写入值对平：
// 写了没登记（红）、登记了没写（红）。列无 CHECK，这是唯一能拦住词表分叉的地方。
//
// 写入侧只有三个来源（grep INSERT INTO audit_logs 实测：user-service repo/audit.go 一处，
// device-service repo/audit_t448.go、audit_t485.go 两处），词形来源对应三种写法，见 patterns。
func TestT485_UpstreamVocabularyIsTheWholeWrittenSet(t *testing.T) {
	root := repoRootT485(t)
	src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(userServiceAuditRepoFile)))
	require.NoError(t, err)

	// 登记处：TargetType 字段那行注释，按 " | " 分隔
	registry := map[string]bool{}
	declared := false
	for _, line := range strings.Split(string(src), "\n") {
		if !strings.Contains(line, "TargetType") || !strings.Contains(line, "|") {
			continue
		}
		declared = true
		comment := line[strings.Index(line, "//")+2:]
		for _, w := range strings.Split(comment, "|") {
			if w = strings.TrimSpace(w); w != "" {
				registry[w] = true
			}
		}
	}
	require.True(t, declared, "词表登记行没读出来（被改写或换行了？）：本用例失去判定力")

	// 写入侧：扫两个服务 internal/ 下的非测试 Go 源码
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`TargetType:\s*"([a-z_]+)"`),          // handler 内直埋字面量
		regexp.MustCompile(`\w*TargetType\w*\s*=\s*"([a-z_]+)"`), // xxxTargetType 常量
		regexp.MustCompile(`\{auditAction\w+,\s*"([a-z_]+)"`),    // user-service 表驱动 auditRoutes
	}
	written := map[string]bool{}
	scanned := 0
	for _, svc := range []string{"services/user-service/internal", "services/device-service/internal"} {
		err := filepath.Walk(filepath.Join(root, filepath.FromSlash(svc)), func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			if filepath.ToSlash(p) == filepath.Join(root, filepath.FromSlash(userServiceAuditRepoFile)) {
				return nil // 登记处自己不算写入侧，否则注释里的词会被自己数进来
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			scanned++
			for _, re := range patterns {
				for _, m := range re.FindAllStringSubmatch(string(b), -1) {
					written[m[1]] = true
				}
			}
			return nil
		})
		require.NoError(t, err)
	}
	require.GreaterOrEqual(t, scanned, 20, "扫描文件数过少 = 路径配错了，全集比对失效")

	unregistered := []string{}
	for w := range written {
		if !registry[w] {
			unregistered = append(unregistered, w)
		}
	}
	phantom := []string{}
	for w := range registry {
		if !written[w] {
			phantom = append(phantom, w)
		}
	}
	sort.Strings(unregistered)
	sort.Strings(phantom)
	assert.Empty(t, unregistered, "代码在写这些作用对象类型，但词表登记处没有它们（本卡缺口的复发形状）：%v", unregistered)
	assert.Empty(t, phantom, "词表登记了代码里没人写的类型：%v", phantom)
	assert.True(t, registry[auditTargetTypeInstallRecord], "全集比对通过但本卡的 install_record 不在其中 = 两侧同时缺，判据没生效")
	assert.GreaterOrEqual(t, len(written), 15, "写入侧只认出 %d 类，少于实测的 17 类 ⇒ 匹配式漏了某种写法", len(written))
}

// TestT485_InstallAuditInputKeepsEmptyOptional 钉入参的形状约定：
// 空串由实现侧 NULLIF 洗成 NULL，调用方不必自己判空（与 user-service 同口径）。
// 一旦有人改成「调用方传 *string」，这两处口径就会分叉，故在此钉住值类型。
func TestT485_InstallAuditInputKeepsEmptyOptional(t *testing.T) {
	in := InstallAuditInput{InstallID: "42"}
	assert.Equal(t, "42", in.InstallID)
	assert.Empty(t, in.OperatorID)
	assert.Nil(t, in.Changed)
}
