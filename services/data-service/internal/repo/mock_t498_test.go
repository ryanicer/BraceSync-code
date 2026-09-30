// T498：注入链路的落库形状与跨服务词表登记 —— 不需要真库，普通 go test 覆盖。
//
// 钉的三件事（每一件都对应一个「库里没有约束兜着、漂了不会响」的面）：
//  1. 审计动词/作用对象词形：audit_logs 两列都是裸 VARCHAR（000001_init_schema.up.sql 无 CHECK），
//     取值全靠跨服务约定；漂了之后审计页筛不出这一行 = 注入没留痕。
//  2. 单帧 INSERT 的第 24 位是来源印章：真实上报与受控注入共用同一条语句形状，
//     一旦有人把 ingest_source 从列清单里删掉，两条链路都会静默写 NULL（NULL 读起来像「未表态」，
//     而验收 3 要求「注入帧可溯源」）。
//  3. 批量补传只写 'real'：注入端点刻意不开批量形态，这条差异要钉在语句里而不是注释里。
//
// 真库那一格（分区传播 / CHECK / 事务回滚）在 repo/mock_t498_integration_test.go。
package repo

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

const (
	// 上游词表登记处：action 的定义在 user-service 埋点文件，target_type 的清单在审计表 owner 侧
	userServiceActionFile = "services/user-service/internal/handler/audit_t252.go"
	userServiceAuditRepo  = "services/user-service/internal/repo/audit.go"
)

// repoRootT498 从本文件上溯 4 级到仓根（与 device-service audit_t448/t485 同一取径）
func repoRootT498(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	dir, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
	require.NoError(t, err)
	return dir
}

func TestT498_AuditWordShapeIsSingularSnakeCase(t *testing.T) {
	assert.Equal(t, "data_modify", auditActionDataModify,
		"动词变了要同步改 user-service 动作词表与 admin-web 筛选下拉（T448 钉过同一约定）")
	assert.Equal(t, "pressure_record", auditTargetTypePressureRecord,
		"作用对象词形变了要同步改上游清单登记")
	// 反证：表名那种复数形不是合法词形（列无 CHECK，写错只会静默筛不出）
	assert.NotEqual(t, "pressure_records", auditTargetTypePressureRecord)
	assert.Equal(t, "unstamped", unstampedSource,
		"哨兵值只出现在审计 detail，不落 ingest_source 列（列 CHECK 只放行 real/mock）")
}

// TestT498_ActionIsInUpstreamVocabulary 注入审计用的动词必须在 user-service 的动作词表里。
func TestT498_ActionIsInUpstreamVocabulary(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRootT498(t), filepath.FromSlash(userServiceActionFile)))
	require.NoError(t, err, "user-service 审计埋点文件被移动或改名时本用例必须响：动词失去上游定义")

	var values []string
	for _, m := range regexp.MustCompile(`(?m)^\tauditAction\w+\s*=\s*"([^"]+)"`).FindAllStringSubmatch(string(raw), -1) {
		values = append(values, m[1])
	}
	// 正对照：探测本身要读出东西，否则「属词表内」退化成「谁都过」
	require.NotEmpty(t, values, "上游动作词读出为空 = 常量块写法变了，本探测要跟着改")
	assert.Contains(t, values, auditActionDataModify,
		"data-service 写的 action=%q 不在上游词表 %v 里：跨服务词表已分叉，审计页筛不到注入留痕",
		auditActionDataModify, values)
	for _, notIn := range []string{"mock_inject", "mock_frame", "ingest_x"} {
		assert.NotContains(t, values, notIn, "负样本 %q 本不该在词表里，它出现说明探测读脏了", notIn)
	}
}

// TestT498_TargetTypeIsRegisteredUpstream 新作用对象必须登记在审计表 owner 侧的全量清单里，
// 否则下一个补型的人照样看不见这一类（T485 立的就是这条规矩）。
func TestT498_TargetTypeIsRegisteredUpstream(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRootT498(t), filepath.FromSlash(userServiceAuditRepo)))
	require.NoError(t, err, "user-service 审计仓储被移动或改名时本用例必须响：词表登记处失去看守")

	src := string(raw)
	assert.Regexp(t, regexp.MustCompile(`\bpressure_record\b`), src,
		"上游清单里没有 pressure_record：本卡的注入留痕在审计表 owner 侧不可见")
	assert.NotRegexp(t, regexp.MustCompile(`\bpressure_records\b`), src,
		"上游清单出现复数形（那是表名，不是对象词形）：与本卡写入值冲突，两处要收口成一个")

	for _, known := range []string{"patient", "install_record", "device"} {
		assert.Regexp(t, regexp.MustCompile(`\b`+known+`\b`), src,
			"清单里连 %q 都读不到 = 这一段没被读进来，上面的判定失效", known)
	}
	assert.NotRegexp(t, regexp.MustCompile(`\borthosis_planx\b`), src, "负样本出现 = 探测恒真")
}

func TestT498_SingleInsertStampsIngestSource(t *testing.T) {
	sql := insertRecordSQL
	assert.Contains(t, sql, "ingest_source",
		"单帧 INSERT 少了来源列：真实链路与注入链路都会静默写 NULL，验收 3「注入可溯源」失效")
	assert.Contains(t, sql, "$24", "来源印章占住第 24 位，删列不改这里要响")
	assert.NotContains(t, sql, "$25", "参数位超出 24 = 列清单与 frameArgs+source 的 23+1 不再对平")
	assert.Equal(t, 23, len(frameArgs("D", "P", PendingFrame{Ts: time.Unix(1734500000, 0)})),
		"frameArgs 的 23 位是列清单的基准，它变了上面两条判据要跟着重算")
	// 幂等键与真实链路同源：注入帧不挤掉真帧靠的就是这把键
	assert.Contains(t, sql, "ON CONFLICT (device_id, ts) DO NOTHING")
}

// TestT498_BatchInsertIsRealOnly 补传通道只属于真实设备：批量语句里来源是字面量 'real'，
// 不占参数位 —— 有人给注入端点补批量形态时，这条会先响。
func TestT498_BatchInsertIsRealOnly(t *testing.T) {
	sql := buildBatchInsertSQL(2)
	assert.Equal(t, 2, strings.Count(sql, "'real')"),
		"批量每行 tuple 都要显式盖 real，实得语句：%s", sql)
	assert.NotContains(t, sql, "ingest_source,$", "批量不给来源留参数位（注入无批量形态）")
	assert.Equal(t, 46, lastParamOrdinal(sql),
		"2 帧 × 23 列 = 最大占位符 $46，与 batchInsertColumns 对平；实得语句：%s", sql)
}

// lastParamOrdinal 取出语句里最大的 $n（不写死数字，改列数时它自己跟着动）
func lastParamOrdinal(sql string) int {
	max := 0
	for _, m := range regexp.MustCompile(`\$(\d+)`).FindAllStringSubmatch(sql, -1) {
		n := 0
		for _, c := range m[1] {
			n = n*10 + int(c-'0')
		}
		if n > max {
			max = n
		}
	}
	return max
}

func TestT498_MockSQLShapes(t *testing.T) {
	// 审计与帧同事务：审计行的 id 要回给调用方，注入现场与留痕才能双向对查
	assert.Contains(t, mockAuditSQL, "INSERT INTO audit_logs")
	assert.Contains(t, mockAuditSQL, "RETURNING log_id")
	assert.Contains(t, mockAuditSQL, "NULLIF($1, '')", "operator 空串落 NULL，与 user-service 同口径")
	// 幂等命中后回查冲突帧的来源（挡住本次注入的那条是谁家的）
	assert.Contains(t, existingFrameAtSQL, "ingest_source")
	assert.Contains(t, existingFrameAtSQL, "WHERE device_id = $1 AND ts = $2")
}

func TestT498_AuditDetailCarriesProvenance(t *testing.T) {
	in := MockFrameInput{
		DeviceID:  "PRS-ML05-RC-20260808001",
		PatientID: "P20260001",
		Frame:     PendingFrame{Ts: time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)},
		Reason:    "boss acceptance: pressure_high trigger",
	}

	fresh := in.auditDetail(MockFrameResult{RecordID: 1})
	assert.Equal(t, model.IngestMock, fresh["ingest_source"], "审计里也要能认出这是注入产物")
	assert.Equal(t, false, fresh["duplicated"])
	assert.NotContains(t, fresh, "conflicting_ingest_source", "未命中幂等时不该出现冲突来源键")
	assert.NotContains(t, fresh, "operator", "未自报操作者时不落空串键（客观那一半是同事务的 ip 列）")
	assert.Equal(t, "PRS-ML05-RC-20260808001", fresh["device_id"])

	// 自报操作者 + 命中幂等（被一条未盖章的存量真帧挡住）
	in.Operator = "ops-script-t498"
	dup := in.auditDetail(MockFrameResult{RecordID: 2, Duplicated: true, ExistingSource: unstampedSource})
	assert.Equal(t, "ops-script-t498", dup["operator"])
	assert.Equal(t, true, dup["duplicated"])
	assert.Equal(t, unstampedSource, dup["conflicting_ingest_source"],
		"冲突行来源必须落进审计：只按 ingest_source=mock 反查会把挡路的那条也读成注入产物")
}
