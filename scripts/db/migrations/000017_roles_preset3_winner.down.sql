-- T262 角色表收口（Winner）回滚：恢复 000016 播的 5 个角色
-- 🔴 回滚只恢复「本迁移删掉的行」；本迁移 up 里补播的 3 个预置登录角色**不删** ——
--    它们是 seed.sql 的既有内容（本迁移只是幂等补齐），且 admins.role_id 外键引用它们
--    （000001_init_schema.up.sql:33；seed.sql:26-28 的 A0001/A0002/A0003），删除即破坏登录链路。
BEGIN;

-- INSERT 段照抄 000016_admin_be2_winner.up.sql:44-55（含 permissions_json，与 roleTemplates 同源）
INSERT INTO roles (role_id, name, description, permissions_json) VALUES
  ('ROLE_SUPER_ADMIN', '超级管理员', '系统全部权限',
   '{"scope":"all","modules":["dashboard","realtime","patients","teams","devices","alerts","comm","orthosis","install","tech","perm","config"]}'),
  ('ROLE_CHIEF_DOCTOR', '主任医师', '患者管理+数据分析+团队管理',
   '{"scope":"all","modules":["dashboard","realtime","patients","teams","alerts","orthosis","install"]}'),
  ('ROLE_ATTENDING_DOCTOR', '主治医师', '患者数据+告警处理+沟通',
   '{"scope":"team","modules":["dashboard","realtime","patients","alerts","comm","orthosis"]}'),
  ('ROLE_REHAB_THERAPIST', '康复师', '患者数据查看+矫形日志+沟通',
   '{"scope":"team","modules":["realtime","patients","alerts","comm","orthosis"]}'),
  ('ROLE_NURSE', '护士', '患者列表查看+基本沟通',
   '{"scope":"team","modules":["patients","comm"]}')
ON CONFLICT (role_id) DO NOTHING;

COMMIT;
