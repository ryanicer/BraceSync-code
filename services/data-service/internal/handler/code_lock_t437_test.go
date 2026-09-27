// T437：数据域「上报 deviceId 不一致」码的字面量锁定 + 跨服务码位唯一性。
//
// 缺陷原貌（T402 丙-1 / 卡面 A1）：data-service 的 CodeDeviceIDMismatch 曾取 20403，
// 与 device-service 的 CodeForbidden 同数字不同契约（那边 HTTP 403 越权、这边 HTTP 400
// 上报身份不一致）。Boss 22:07 要求先只读确认设备直连侧有无按 20403 数字分支，Andy 已交
// 只读确认包（docs/tasks/andy/T402-c1-20403-readonly/），PM 2026-09-28 02:04 裁定方向：
// 设备域独占 20403，data 侧改为本域形状「域号 3 + 0 + HTTP 状态三位 400」= 30400。
//
// 为什么要钉字面量：本域既有断言全是符号对符号（model_test.go:98、record_test.go:411、
// handler_test.go:196 均写成 assert.Equal(t, model.CodeDeviceIDMismatch, ...)），
// 改回 20403 或改成别的值，用例跟着常量走、逐格仍绿 ⇒ 三处各补一次字面量锁。
// 形状照 code_lock_t402_test.go（常量层 + 线上报文层）与 device-service 的
// code_lock_t389_test.go 先例。
//
// 本文件不锁设备域那一格：device-service 的 20403 由 code_lock_t389_test.go 看守，
// 那里的 t389CodeForbidden / "code":20403 是本次改动的禁区，一笔都不许跟着动。
package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

// 刻意写成字面量，禁止替换成 model.* —— 「用符号断言符号」正是本文件要堵掉的形状。
const (
	t437DataCodeDeviceIDMismatch = 30400 // 数据域：域号 3 + 0 + HTTP 400
	t437HTTPBadRequest           = 400
	t437DeviceCodeForbidden      = 20403 // 设备域越权码，本卡起全仓唯一持有者只剩 device-service
)

// ── 1. 常量层：码值与 HTTP 状态各钉一次，并锁「不再等于 20403」 ──────────

func TestT437_DataDeviceIDMismatchCodeIsLockedToNumericLiteral(t *testing.T) {
	assert.Equal(t, t437DataCodeDeviceIDMismatch, model.CodeDeviceIDMismatch,
		"数据域上报身份不一致业务码契约值（T437 收为域号 3 + 0 + HTTP 三位 400）")

	// 负锁：撞值就是本卡要修的东西，退回 20403 必红（不依赖上一条的镜像值）
	assert.NotEqual(t, t437DeviceCodeForbidden, model.CodeDeviceIDMismatch,
		"T437 之后 20403 归设备域独占，本域不得再取该值")

	appErr := model.ErrDeviceIDMismatch()
	require.NotNil(t, appErr)
	assert.Equal(t, t437DataCodeDeviceIDMismatch, appErr.Code, "ErrDeviceIDMismatch 必须携带被锁定的业务码")
	assert.Equal(t, t437HTTPBadRequest, appErr.HTTPStatus,
		"HTTP 状态契约值仍是 400（甲-1 只改业务码，不动状态码）")
}

// ── 2. 线上层：头体不一致的拒绝响应报文原文逐字带 code:30400 ─────────────

func TestT437_DeviceIDMismatchWireBodyCarriesLiteralCode(t *testing.T) {
	srv := newTestServer(nil)
	body := validFrameBody(time.Now().Add(-time.Minute))

	w := doReq(t, srv, http.MethodPost, "/api/v1/device/records", body,
		map[string]string{"X-Device-Id": "PRS-ML05-RC-OTHER"})
	raw := w.Body.String()
	t.Logf("线上报文原文：%s", raw)

	assert.Equal(t, t437HTTPBadRequest, w.Code, "HTTP 状态不得随业务码一起改动，body=%s", raw)
	assert.Contains(t, raw, `"code":30400`,
		"响应报文原文须含字面 code:30400（只改常量或只改序列化路径都会在这里露出来）")
	assert.NotContains(t, raw, `"code":20403`, "旧撞值不得再出现在本域响应报文里，body=%s", raw)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &decoded), "body=%s", raw)
	got, ok := decoded["code"]
	require.True(t, ok, "统一响应体须有 code 字段，body=%s", raw)
	assert.Equal(t, float64(t437DataCodeDeviceIDMismatch), got, "code 解码后须为 %d", t437DataCodeDeviceIDMismatch)
}

// ── 3. 扫描器自检：先证明这条读法能认出码，再谈「唯一」 ──────────────────
//
// 「扫不到」与「只有一处」在断言里同形。fixture 用已知样本锁住读法本身，
// 免得仓根路径写错、或常量块换写法（iota / 无 Code 前缀）时整条门禁静默放行。

type t437CodeDecl struct {
	service string
	name    string
	value   int
}

// t437AssignRe 匹配顶层常量赋值行 `NAME = 12345`（值为 4-5 位纯数字）。
var t437AssignRe = regexp.MustCompile(`(?m)^[ \t]*([A-Za-z_][A-Za-z0-9_]*)[ \t]*=[ \t]*([0-9]{4,5})\b`)

// t437ParseCodeDecls 从一份源码文本里抽出码声明：只保留标识符含 "Code" 的那些
// （覆盖各服务两种命名：CodeForbidden / ErrorCodeForbidden）。注释行不参与匹配
// （整行须以标识符开头，故 "// CodeBaz = 20403" 不会被误认成声明）。
func t437ParseCodeDecls(service, src string) []t437CodeDecl {
	var out []t437CodeDecl
	for _, m := range t437AssignRe.FindAllStringSubmatch(src, -1) {
		if !strings.Contains(m[1], "Code") {
			continue
		}
		v := 0
		for _, r := range m[2] {
			v = v*10 + int(r-'0')
		}
		out = append(out, t437CodeDecl{service: service, name: m[1], value: v})
	}
	return out
}

// t437ScannedFile 判定扫描范围：services/ 下的非测试 Go 源（测试里的字面量锁正是本卡要保护的产物）。
func t437ScannedFile(rel string) bool {
	r := filepath.ToSlash(rel)
	if !strings.HasPrefix(r, "services/") || !strings.HasSuffix(r, ".go") {
		return false
	}
	return !strings.HasSuffix(r, "_test.go")
}

func TestT437_CodeDeclScannerHasPositiveControl(t *testing.T) {
	// 已知有解：三真两假，读法坏了这里先响
	fixture := "const (\n" +
		"\tCodeOK = 0\n" +
		"\tCodeFoo = 30400 // 真：带行尾注释\n" +
		"\tCodeBar = 20403\n" +
		"\t// CodeBaz = 20403 注释行，不算声明\n" +
		"\tErrorCodeQux = 61003\n" +
		"\tCodeTooLongValue = 1234567\n" +
		"\tPlainConst = 20403\n" +
		")\n"
	got := t437ParseCodeDecls("svc-fixture", fixture)
	var names []string
	for _, d := range got {
		names = append(names, d.name)
	}
	assert.Equal(t, []string{"CodeFoo", "CodeBar", "ErrorCodeQux"}, names,
		"扫描器须认出带/不带行尾注释的 Code* 赋值、忽略注释行与非 Code 标识符")

	// 已知有值：30400 / 20403 都要被解析成整数，否则后面的「谁持有」判据是空的
	byName := map[string]int{}
	for _, d := range got {
		byName[d.name] = d.value
	}
	assert.Equal(t, 30400, byName["CodeFoo"])
	assert.Equal(t, 20403, byName["CodeBar"])

	// 已知扫描范围判据本身不空转
	assert.True(t, t437ScannedFile("services/data-service/internal/model/model.go"))
	assert.False(t, t437ScannedFile("services/data-service/internal/handler/code_lock_t437_test.go"))
	assert.False(t, t437ScannedFile("apps/admin-web/src/api/code.ts"))
}

// ── 4. 跨服务唯一性：20403 仅设备域持有、30400 仅数据域持有 ───────────────
//
// 为什么用源码扫描而不是 Go 断言：八个服务是 go.work 下的独立模块，internal/ 不可跨模块
// import，编译期拿不到对面的常量（先例：CI 的 route-check 也是 bash 扫源码）。
// 覆盖范围：services/ 下全部非测试 Go 源。不含 apps/、packages/（三端消费方已实测零引用，
// 见交件单），故这条锁的是「后端各域不再共用同一码位」。

func TestT437_20403And30400AreEachHeldByExactlyOneService(t *testing.T) {
	decls, files := t437ScanServiceCodeDecls(t)
	t.Logf("扫描面：services/ 非测试 Go 源 %d 个文件，解析出含 Code 的 4-5 位码声明 %d 条", files, len(decls))

	// 非空转对照：读不到源码就直接判红，别把「路径写错」读成「全仓唯一」
	require.Greater(t, files, 100, "扫描到的 services/ Go 源文件数异常偏小，判据失效")
	require.Greater(t, len(decls), 30, "未从源码里解析出预期量级的码声明，判据失效")
	// 已知有解对照：四个域的越权码都须被看见（其中 20403 的持有者正是下面要断言的那一条）
	for _, anchor := range []int{10403, 20403, 30403, 50403} {
		assert.NotEmpty(t, t437Holders(decls, anchor), "锚点码 %d 必须能被扫到，扫不到说明读法坏了", anchor)
	}

	assert.Equal(t, []string{"device-service"}, t437Holders(decls, t437DeviceCodeForbidden),
		"T437 裁定：20403 由设备域独占（data 侧已改 30400）")
	assert.Equal(t, []string{"CodeForbidden"}, t437DeclNames(decls, t437DeviceCodeForbidden, "device-service"),
		"设备域那一格的常量名须仍是 CodeForbidden")
	assert.Empty(t, t437DeclNames(decls, t437DeviceCodeForbidden, "data-service"),
		"data-service 不得再有任何码取 20403")

	assert.Equal(t, []string{"data-service"}, t437Holders(decls, t437DataCodeDeviceIDMismatch),
		"30400 须由数据域独占（本卡新增码位的占用方唯一）")
	assert.Equal(t, []string{"CodeDeviceIDMismatch"}, t437DeclNames(decls, t437DataCodeDeviceIDMismatch, "data-service"),
		"数据域那一格的常量名须仍是 CodeDeviceIDMismatch")
}

// t437RepoRoot 从本文件上溯 4 级到仓根（services/data-service/internal/handler → 仓根）。
func t437RepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "取不到本测试文件路径")
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..")
}

// t437ScanServiceCodeDecls 遍历仓内 services/ 下的非测试 Go 源，返回全部码声明与扫到的文件数。
// 服务名取 services/ 之后的那一段路径。
func t437ScanServiceCodeDecls(t *testing.T) ([]t437CodeDecl, int) {
	t.Helper()
	root := t437RepoRoot(t)
	servicesDir := filepath.Join(root, "services")

	var decls []t437CodeDecl
	files := 0
	err := filepath.Walk(servicesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if !t437ScannedFile(rel) {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		files++
		parts := strings.Split(filepath.ToSlash(rel), "/")
		require.GreaterOrEqual(t, len(parts), 3, "路径 %s 不是 services/<svc>/... 形状", rel)
		decls = append(decls, t437ParseCodeDecls(parts[1], string(raw))...)
		return nil
	})
	require.NoError(t, err, "扫描 services/ 源码失败（目录被移动或改名时本用例必须响）")
	return decls, files
}

// t437Holders 返回把 value 作为自己码位声明的服务清单（去重、字典序，保证失败信息可复现）。
func t437Holders(decls []t437CodeDecl, value int) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range decls {
		if d.value == value && !seen[d.service] {
			seen[d.service] = true
			out = append(out, d.service)
		}
	}
	sort.Strings(out)
	return out
}

// t437DeclNames 返回某服务里取该码位的常量名清单。
func t437DeclNames(decls []t437CodeDecl, value int, service string) []string {
	var out []string
	for _, d := range decls {
		if d.value == value && d.service == service {
			out = append(out, d.name)
		}
	}
	sort.Strings(out)
	return out
}
