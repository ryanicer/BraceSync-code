// T498 开关真值表（进程级门控）。
//
// 为什么钉在 cmd/server：ALLOW_MOCK_INGEST 的判定只有两条腿 —— 这里的真值表 + service 层的
// 每请求复判。service 层那条已有用例（service/mock_ingest_t498_test.go），这条若没人测，
// 「APP_ENV 拼错就按生产对待」这件事就只剩注释在守着，而它守的是「生产起来忘了关」这一格。
//
// main 里的 log.Fatal 不可测，所以判定抽成了纯函数 mockIngestGate；
// 本文件同时钉「main 真用了这个函数」，免得以后有人把判定复制回 main 而用例仍绿。
package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestT498_MockIngestGateTruthTable(t *testing.T) {
	cases := []struct {
		name       string
		appEnv     string
		allow      string
		wantOn     bool
		wantReject bool
	}{
		{"默认关：ALLOW_MOCK_INGEST 未设", "staging", "", false, false},
		{"显式 false", "staging", "false", false, false},
		{"非真值写法一律算关（别把 yes/on 读成开）", "staging", "yes", false, false},
		{"大小写不严谨的 on 也算关（只认 true/1）", "staging", "TRUE", false, false},
		{"staging + true 才开", "staging", "true", true, false},
		{"dev + 1 也算开", "dev", "1", true, false},
		{"test + true", "test", "true", true, false},
		{"production 设了就拒绝", "production", "true", false, true},
		{"APP_ENV 未设：envOr 兜底是 production ⇒ 拒绝", "production", "1", false, true},
		{"APP_ENV 拼错（prodction）：白名单外 ⇒ 按生产拒绝", "prodction", "true", false, true},
		{"APP_ENV 空串：白名单外 ⇒ 拒绝", "", "true", false, true},
		{"环境大小写与空白容错（STAGING / ' staging '）", "  STAGING ", "true", true, false},
		{"拒绝时也没被开起来（on 与 reject 互斥）", "production", "true", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			on, reject := mockIngestGate(c.appEnv, c.allow)
			assert.Equal(t, c.wantOn, on)
			if c.wantReject {
				require.NotEmpty(t, reject, "配置矛盾必须返回原因，调用方才有 log.Fatal 的判据")
				assert.False(t, on, "拒绝态不得同时把开关打开")
			} else {
				assert.Empty(t, reject)
			}
		})
	}
}

// TestT498_NonProdWhitelistIsExactlyThree 白名单成员钉死：
// 加一个环境（如 pre/prod-cn）是「扩大可注入面」的动作，不该在顺手改配置时发生。
func TestT498_NonProdWhitelistIsExactlyThree(t *testing.T) {
	keys := make([]string, 0, len(nonProdEnvs))
	for k := range nonProdEnvs {
		keys = append(keys, k)
	}
	assert.ElementsMatch(t, []string{"dev", "test", "staging"}, keys,
		"可注入 mock 的环境集合变了要同步改运维文档与卡内待裁项")
	assert.Len(t, nonProdEnvs, 3)
}

// TestT498_MainActuallyCallsTheGate 钉「判定没被复制回 main」：
// main 里必须调用 mockIngestGate，且 reject 非空那一支必须自己 log.Fatal 阻断启动，
// 否则本文件测的是个没人调的函数（假绿）。
//
// Fatal 只在 `if reject != "" { ... }` 那一支里找，不认全文件的 log.Fatal：
// main 里 DB ping 失败也是 Fatal，按全文件找会恒真。
func TestT498_MainActuallyCallsTheGate(t *testing.T) {
	root, err := os.Getwd()
	require.NoError(t, err)
	src, err := os.ReadFile(filepath.Join(root, "main.go"))
	require.NoError(t, err)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", src, parser.ParseComments)
	require.NoError(t, err, "main.go 语法坏了时本用例先响：AST 断言失去判定力")

	callsGate := false
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if fn, ok := call.Fun.(*ast.Ident); ok && fn.Name == "mockIngestGate" {
				callsGate = true
			}
		}
		return true
	})
	require.True(t, callsGate, "main 不再调用 mockIngestGate：判定被内联，本用例测不到生效的那条路")

	rejectBranch := findRejectBranch(file)
	require.NotNil(t, rejectBranch, "main 里没有 `if reject != \"\"` 这一判：矛盾配置不再阻断启动（验收 1 失守）")
	assert.True(t, branchHasFatal(rejectBranch),
		"reject 分支里没有 log.Fatal：生产环境设了 ALLOW_MOCK_INGEST 只会留一条日志照常起来")
}

// findRejectBranch 返回 cond 形如 `reject != ""` 的 if 语句体
func findRejectBranch(file *ast.File) *ast.BlockStmt {
	var found *ast.BlockStmt
	ast.Inspect(file, func(n ast.Node) bool {
		stmt, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		bin, ok := stmt.Cond.(*ast.BinaryExpr)
		if !ok || bin.Op != token.NEQ {
			return true
		}
		if id, ok := bin.X.(*ast.Ident); ok && id.Name == "reject" {
			found = stmt.Body
		}
		return true
	})
	return found
}

func branchHasFatal(body *ast.BlockStmt) bool {
	has := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Fatal" {
			has = true
		}
		return true
	})
	return has
}
