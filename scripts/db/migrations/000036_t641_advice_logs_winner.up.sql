-- T641 「康复建议」新表 advice_logs（设计稿 §六① 字段模型，R1-R7 甲档）
--
-- 口径依据（PRD §7A.12 + §7D.8 V3.43 + docs/tasks/peter/T637-附-医生建议功能设计稿.md §六/§十一）：
--   留言板模式（Boss 13:12 拍板 1）：提交即记录，患者打开页面才拉取 ⇒ 无推送、无已读态、患者侧零交互，
--   所以本表刻意不放任何 reply/read/is_read 列（§六① 第四行判定：现网 is_read/read_at 全仓 0 命中）。
--
-- 逐列先例坐标（均为 000001_init_schema.up.sql）：
--   advice_id        ← :218 plan_id / :228 log_id / :242 feedback_id（BIGINT IDENTITY 同形）
--   patient_id       ← :219 / :229 / :243（VARCHAR(32) + REFERENCES patients）
--   author_doctor_id ← :220 orthosis_plans.doctor_id（存引用不存快照 = R5 甲，职称读时 JOIN doctors.title）
--   content          ← :221 方案正文同形用 TEXT；长度上限走应用层 rune 口径（R2 甲 = 500，与
--                      :245 feedbacks.content VARCHAR(500) 同档），不在列上写 VARCHAR(n) ——
--                      §六① 判定表第一条：VARCHAR(200) 那档是给「一句感受回复」的，方案/建议正文是长文本。
--   created_at       ← :223
--   updated_at       ← :83 patients.updated_at 同形（注释自陈「编辑时由应用层刷新」，故不带 ON UPDATE 机制）
--
-- 不要 version 列：orthosis_plans.version（:222）表达的是「新版替换旧版」的版本链，
-- 建议流是时间流（Boss 拍板 6 是「编辑 + 删除」，不是「另起一版」），抄过来会凭空造出 v1/v2 并存的历史。
--
-- 删除语义 = R3 甲「硬删 + 审计留痕」（handler 侧 audit_t252.go 路由表登记 DELETE 一行），
-- 全仓 deleted_at 0 命中（设计稿 §六② R3 的尺），本表不开全库第一枚软删列。
BEGIN;

CREATE TABLE IF NOT EXISTS advice_logs (
  advice_id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  patient_id        VARCHAR(32)  NOT NULL REFERENCES patients(patient_id),
  author_doctor_id  VARCHAR(32)  NOT NULL REFERENCES doctors(doctor_id),
  content           TEXT         NOT NULL,
  created_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
  updated_at        TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- 患者端时间轴与后台历史列表都是「按患者取、按时间倒序」，同 idx_plans_patient（:225）/ idx_feeling_patient_date（:239）形状
CREATE INDEX IF NOT EXISTS idx_advice_patient ON advice_logs (patient_id, created_at DESC);

COMMENT ON TABLE advice_logs IS
  'T641 康复建议（留言板模式，PRD §7A.12）：医护对患者单向输出，纯文本 rune<=500，'
  '作者身份存 doctors 引用（职称读时 join，R5 甲），编辑/删除仅作者本人（R7 甲，谓词在 SQL 里），'
  '删除为硬删 + audit_logs 留痕（R3 甲）。无 version / 无 reply / 无 read 列。';

COMMIT;
