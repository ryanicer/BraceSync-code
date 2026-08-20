-- BraceSync P0 问题修复迁移（golang-migrate up）
-- 对齐：docs/ 已拍板）
-- 落地：P0-2 绑定历史表、P0-3 UNIQUE 约束、P0-4 age→birth_date 第一步

BEGIN;

-- ============ P0-2：新增 device_bindings 绑定历史表 ============
-- 解决 patients⇄devices 双向引用跨服务无事务问题
-- 唯一约束：同一设备同时仅一条有效绑定（WHERE unbind_at IS NULL）
-- patients.device_id 降级为展示字段；devices.patient_id 保留为当前绑定冗余
CREATE TABLE device_bindings (
  binding_id   BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  device_id    VARCHAR(48) NOT NULL,
  patient_id   VARCHAR(32) NOT NULL,
  bind_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  unbind_at    TIMESTAMPTZ,              -- NULL=当前有效
  reason       VARCHAR(16),              -- install / rebind / unbind
  operator_id  VARCHAR(32)
);
CREATE UNIQUE INDEX uk_bindings_active ON device_bindings(device_id) WHERE unbind_at IS NULL;
CREATE INDEX idx_bindings_patient ON device_bindings(patient_id);

-- ============ P0-3：install_records.baseline_id 加 UNIQUE ============
-- 保证一次安装至多一条校准基线（1:1），消除 DB 层无约束风险
-- 流程约定：先建 install（baseline_id 可空）→ 校准完成后回填 baseline_id 并插入 baselines
ALTER TABLE install_records ADD CONSTRAINT uk_install_baseline UNIQUE (baseline_id);

-- ============ P0-4：patients.age → birth_date（第一步：加列停用） ============
-- birth_date 可空（成人/信息不全时可缺省），age 查询时由 birth_date 相对当前日期计算
-- TODO: 后续版本删除 age 列（两步迁移第二步），当前 age 列保留但停用，业务代码改读 birth_date
ALTER TABLE patients ADD COLUMN birth_date DATE;
COMMENT ON COLUMN patients.birth_date IS '出生日期（可空，替代 age 静态快照；未成年人判定依据，PIPL 合规关联 consents）';
COMMENT ON COLUMN patients.age IS '[DEPRECATED] 静态年龄快照，已由 birth_date 替代，查询时动态计算；待后续迁移删除';

COMMIT;
