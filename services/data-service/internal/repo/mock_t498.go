// T498 受控 mock 帧注入的落库通路：一条注入帧 = 一行 pressure_records + 一行 audit_logs，
// 两者必须在同一个事务里，全成或全不成。
//
// 为什么审计要同事务、而不是像 user-service 那样「附带动作只记 WARN」：
// 真实上报链路的留痕丢了不影响数据可信度，而这里的产出**本身就是假数据**——
// 一旦帧落库而审计行没落，库里就多出一条既无来源说明、又查不到是谁注入的记录，
// 「mock 与真实不互污染」这条验收前提直接失效。所以这里反过来：审计失败必须回滚帧。
//
// audit_logs 的 owner 是 user-service（T252），仓内没有跨服务写审计的 HTTP 通道，
// 而各服务连的是同一个库 ⇒ 同库直写（先例：device-service repo/audit_t448.go、audit_t485.go）。
// 动词沿用 T252 词表的 data_modify，不新造（T448/T485 同选择，T448 还把这个约定钉成了用例）。
package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
)

const (
	// auditActionDataModify T252 五值动词表内（user-service handler/audit_t252.go:37），
	// admin-web 操作日志筛选下拉也收录该值（apps/admin-web/src/pages/settings/index.vue:189）。
	// 新造一个 mock_inject 会让这一行在审计页上筛不出来，等于没留痕。
	auditActionDataModify = "data_modify"
	// auditTargetTypePressureRecord 对象类型词形：单数 snake_case，与同族 device /
	// install_record / orthosis_plan 一致（target_type 列无 CHECK，全靠约定 ⇒ 见 mock_t498_test.go 的字面锁）。
	auditTargetTypePressureRecord = "pressure_record"
)

// MockFrameInput 一次注入的全部输入（调用方 = service 层，已过门禁与业务校验）。
// Operator 是注入方自报的标识：/internal/* 不经 gateway，没有 JWT 注入的 X-User-Id，
// 服务端无法验证它，只能原样落进 detail.operator（同事务的 ip 列是客观那一半）。
type MockFrameInput struct {
	DeviceID  string
	PatientID string
	Frame     PendingFrame
	Operator  string
	Reason    string
	IP        string
}

// MockFrameResult 注入结果。Duplicated=false 时 RecordID 是本次新行；
// =true 时该 (device_id, ts) 已有帧（真实帧或既往注入帧），RecordID 是那一条的 id，
// 本次只补审计行、不改写既有帧（冲突行的来源见 existingFrameAtSQL 的回查）。
type MockFrameResult struct {
	RecordID   int64
	Duplicated bool
	AuditLogID int64
	// ExistingSource 仅 Duplicated=true 时有值：挡住本次注入的那条帧的来源
	// （real / mock / unstamped），落进审计 detail 供事后判读。
	ExistingSource string
}

// existingFrameAtSQL 幂等命中后回查冲突帧：id 供响应与审计定位，ingest_source 供判读
// 「这条注入被哪一类帧挡住了」。
const existingFrameAtSQL = `
SELECT record_id, ingest_source
FROM pressure_records
WHERE device_id = $1 AND ts = $2`

// mockAuditSQL 与 device-service 那两条同库直写同形状（空串落 NULL），多一个 RETURNING log_id：
// 响应体要把审计行 id 回给调用方，注入现场与留痕才能双向对查。
const mockAuditSQL = `
INSERT INTO audit_logs (operator_id, operator_role, action, target_type, target_id, detail, ip)
VALUES (NULLIF($1, ''), NULLIF($2, ''), $3, $4, $5, $6::jsonb, NULLIF($7, ''))
RETURNING log_id`

// InsertMockFrame 单事务写入注入帧 + 审计行。
// 幂等（ON CONFLICT DO NOTHING）与真实上报共用同一把键 (device_id, ts)：
// 注入帧不挤掉真帧，真帧也不会被注入帧改写 —— 混库风险由这条键挡住，来源由 ingest_source 区分。
func (r *RecordRepo) InsertMockFrame(ctx context.Context, in MockFrameInput) (MockFrameResult, error) {
	var res MockFrameResult

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return res, fmt.Errorf("begin mock ingest tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	recordID, inserted, err := insertRecord(ctx, tx, in.DeviceID, in.PatientID, in.Frame, model.IngestMock)
	if err != nil {
		return res, err
	}
	res.Duplicated = !inserted
	if inserted {
		res.RecordID = recordID
	} else {
		var existingSource *string
		if err := tx.QueryRow(ctx, existingFrameAtSQL, in.DeviceID, in.Frame.Ts).Scan(&res.RecordID, &existingSource); err != nil {
			return res, fmt.Errorf("lookup conflicting frame: %w", mapPGError(err))
		}
		// 冲突行来源未盖章（迁移上线前的存量帧）时读作 unstamped：这格必须留在审计 detail 里，
		// 否则事后只看 ingest_source=mock 会把「挡住注入的那条」也读成注入产物。
		res.ExistingSource = unstampedSource
		if existingSource != nil {
			res.ExistingSource = *existingSource
		}
	}

	payload, err := json.Marshal(in.auditDetail(res))
	if err != nil {
		return res, fmt.Errorf("marshal mock audit detail: %w", err)
	}
	if err := tx.QueryRow(ctx, mockAuditSQL, in.Operator, "", auditActionDataModify,
		auditTargetTypePressureRecord, strconv.FormatInt(res.RecordID, 10), string(payload), in.IP).
		Scan(&res.AuditLogID); err != nil {
		return res, fmt.Errorf("write mock ingest audit: %w", mapPGError(err))
	}

	if err := tx.Commit(ctx); err != nil {
		return res, fmt.Errorf("commit mock ingest tx: %w", err)
	}
	return res, nil
}

// unstampedSource 冲突行 ingest_source 为 NULL 时的记法（值域外的记法，只出现在审计 detail，不落列）。
const unstampedSource = "unstamped"

// auditDetail 审计行的结构化内容：description 是审计页唯一渲染的那句（其余键供反查）。
func (in MockFrameInput) auditDetail(res MockFrameResult) map[string]any {
	d := map[string]any{
		"description":   "T498 受控 mock 帧注入（ingest_source=mock）",
		"ingest_source": model.IngestMock,
		"reason":        in.Reason,
		"device_id":     in.DeviceID,
		"patient_id":    in.PatientID,
		"frame_ts":      in.Frame.Ts.UTC().Format(time.RFC3339),
		"duplicated":    res.Duplicated,
	}
	if in.Operator != "" {
		d["operator"] = in.Operator
	}
	if res.Duplicated {
		d["conflicting_ingest_source"] = res.ExistingSource
	}
	return d
}

// 编译期确认：pgx.Tx 满足本包抽出的 rowQuerier（真实上报与注入两条链路共用一条 INSERT 形状）。
var _ rowQuerier = pgx.Tx(nil)
