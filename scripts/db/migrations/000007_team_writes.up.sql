-- T059: 团队管理写功能——teams 表扩展列（leader/description/status）
-- 对齐：docs/tasks/ella/T059-团队管理测试规格.md
-- 用途：teams 表加 leader（负责人 doctor_id）、description（团队描述）、status（状态）
--      支撑 T059 团队详情返回 TeamDetailRow（leader/leaderName/description/status/createdAt）
-- owner：user-service

BEGIN;

ALTER TABLE teams ADD COLUMN IF NOT EXISTS leader      VARCHAR(32);                 -- 负责人 doctor_id（FK 软引用，不做约束）
ALTER TABLE teams ADD COLUMN IF NOT EXISTS description VARCHAR(255);                -- 团队描述（≤200 字符，handler 层校验）
ALTER TABLE teams ADD COLUMN IF NOT EXISTS status      VARCHAR(16) NOT NULL DEFAULT 'active'
    CHECK (status IN ('active','deleted'));

COMMENT ON COLUMN teams.leader      IS '负责人 doctor_id（T059 团队管理写功能）';
COMMENT ON COLUMN teams.description IS '团队描述（T059，≤200 字符）';
COMMENT ON COLUMN teams.status      IS '团队状态（T059，active/deleted，预留软删除）';

COMMIT;
