-- T274 告警流程画布（2.3 运行态画布 + 2.4 拖拽设计器）：flow_* 四表
-- 依据：docs/architecture/flow-canvas-architecture-selection.md §4.3（T254 选型 = LogicFlow 2.x）
-- 契约：docs/api/api-contracts.ts（docs PR #179）
-- owner：四表均归 user-service —— admin CRUD 分层范式（gin + Store + audit + envelope）在该服务，
--        且 scripts/deploy/check-routes.sh 只比对 gateway ↔ user-service/handler.go 两侧路由。
-- 🔴 号位：本卡取 000020；000018/000019 由 #136（T257/T203）预留，无论其是否已合并都不占用。

BEGIN;

-- ── 2.4 设计器产物：流程模板 ──────────────────────────────────────────
-- nodes/edges 存 lf.getGraphData() 原样（前端不做二次转换）；后端只解析
-- node.id / node.text.value / edge.sourceNodeId+targetNodeId，其余不改写。
CREATE TABLE flow_template (
    template_id   VARCHAR(32)  PRIMARY KEY,
    name          VARCHAR(64)  NOT NULL,
    nodes         JSONB        NOT NULL DEFAULT '[]'::jsonb,
    edges         JSONB        NOT NULL DEFAULT '[]'::jsonb,
    -- 创建人可为 admin/doctor/cs/technician 任一账号 ⇒ 不建外键（口径同 audit_logs.operator_id）
    creator       VARCHAR(32)  NOT NULL,
    version       INT          NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX idx_flow_template_updated ON flow_template (updated_at DESC);

COMMENT ON TABLE  flow_template IS 'T274 2.4 流程模板（设计器产物）';
COMMENT ON COLUMN flow_template.nodes IS 'LogicFlow 2.x graphData.nodes 原样，后端不解析业务属性；节点类别约定在 properties.kind';
COMMENT ON COLUMN flow_template.edges IS 'LogicFlow 2.x graphData.edges 原样；端点键名 2.x 为 sourceNodeId/targetNodeId';
COMMENT ON COLUMN flow_template.version IS '每次 PUT 保存 +1，仅作展示；不作乐观锁（并发后写覆盖前写）';
COMMENT ON COLUMN flow_template.name IS '无唯一索引（同 roles 先例）：重名由应用层查重回 409，加唯一约束会让本迁移在既有库上失败';

-- ── 运行态实例：一条告警对应一个流程实例 ──────────────────────────────
CREATE TABLE flow_instance (
    instance_id      VARCHAR(32)  PRIMARY KEY,
    template_id      VARCHAR(32)  NOT NULL REFERENCES flow_template (template_id),
    -- 跨 owner 引用（alerts 归 alert-service）：只建外键不读列，与本仓既有惯例一致
    -- （000001 的 alerts.patient_id REFERENCES patients、000009 review_records.report_file_id）
    alert_id         BIGINT       NOT NULL UNIQUE REFERENCES alerts (alert_id),
    current_node_id  VARCHAR(64),
    status           VARCHAR(12)  NOT NULL DEFAULT 'running'
                       CHECK (status IN ('running', 'completed', 'terminated')),
    started_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    ended_at         TIMESTAMPTZ
);

CREATE INDEX idx_flow_instance_template ON flow_instance (template_id);

COMMENT ON TABLE  flow_instance IS 'T274 运行态流程实例（每条告警一个，2.3 画布的宿主行）';
COMMENT ON COLUMN flow_instance.current_node_id IS '便捷指针：最近一次推进到的节点。并行/汇聚下可能有多个 current，画布着色必须以 flow_node_state.status=''current'' 集合为准';
COMMENT ON COLUMN flow_instance.alert_id IS 'UNIQUE = 一条告警一个实例；重复启动由应用层回 409 + 既有实例（幂等跳转）。外键拦截不存在的 alertId → 400';

-- ── 节点运行状态：2.3 画布着色数据源 ──────────────────────────────────
CREATE TABLE flow_node_state (
    instance_id   VARCHAR(32)  NOT NULL REFERENCES flow_instance (instance_id) ON DELETE CASCADE,
    node_id       VARCHAR(64)  NOT NULL,   -- LogicFlow 节点 id，模板内唯一
    status        VARCHAR(8)   NOT NULL DEFAULT 'todo'
                    CHECK (status IN ('done', 'current', 'todo', 'skipped')),
    operator      VARCHAR(32),
    operated_at   TIMESTAMPTZ,
    remark        VARCHAR(512),
    attachments   JSONB        NOT NULL DEFAULT '[]'::jsonb,
    assignee      VARCHAR(32),
    PRIMARY KEY (instance_id, node_id)
);

COMMENT ON TABLE  flow_node_state IS 'T274 节点运行状态；实例启动时按模板节点全量生成为 todo';
COMMENT ON COLUMN flow_node_state.attachments IS 'file-service fileId 字符串数组，不建外键（口径同 review_records.report_file_id）';
COMMENT ON COLUMN flow_node_state.assignee IS '当前指派处理人。启动时为 NULL（后端不解析模板 properties.assignee），仅 action=''transfer'' 改写';

-- ── 节点操作记录：2.3 处理时间线数据源 ────────────────────────────────
CREATE TABLE flow_node_action (
    action_id        BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    instance_id      VARCHAR(32)  NOT NULL REFERENCES flow_instance (instance_id) ON DELETE CASCADE,
    node_id          VARCHAR(64)  NOT NULL,
    action           VARCHAR(12)  NOT NULL
                       CHECK (action IN ('confirm', 'reject', 'transfer', 'urge')),
    operator         VARCHAR(32)  NOT NULL,
    remark           VARCHAR(512),
    attachments      JSONB        NOT NULL DEFAULT '[]'::jsonb,
    target_operator  VARCHAR(32),
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- 时间线按实例取全量、正序（contract：不分页，单实例操作数 <100）
CREATE INDEX idx_flow_node_action_instance ON flow_node_action (instance_id, action_id);

COMMENT ON TABLE  flow_node_action IS 'T274 节点操作流水（append-only，时间线与设计稿底部记录）';
COMMENT ON COLUMN flow_node_action.target_operator IS '仅 action=''transfer'' 有值：转派目标账号 ID';

COMMIT;
