-- T396 给 files.owner_type 补枚举 CHECK（Boss 裁 待Boss裁决.md 第二十四条 (a)：补 CHECK + 先清洗）
-- 现象：000006:13 建列时只写了 VARCHAR(64) NOT NULL，注释「Entity type: InstallRecord, Patient, etc.」
--       —— 同表的 file_type / status 两列都有 CHECK，唯独 owner_type 没有。
--       Andy T391 取证（docs 仓 docs/tasks/andy/T391-evidence/，origin/main cbade26）：
--       staging public.files 当时共 3 行，owner_type 取值为大写 'Patient'(2) + 'review'(1)，
--       与代码实际写入的小写 'patient' 逐字不等 ⇒ 这 3 行在按 owner_type 反查归属的读路径上恒不可见；
--       三个 owner_id 在 patients/doctors/teams 里均查无。PM 已按授权删净（删后复核零行）。
-- 根因：写侧无枚举校验。presign 的 owner_type 直接取请求体（仅 binding:"required" 挡空串），
--       staff 角色还允许客户端自报（T261 身份单一来源只把非 staff 强制成 'patient'，
--       file_handler.go:148-151），所以任意大小写/任意串都能落库。
-- 修法：只加约束、不改列型、不动数据、不删改历史迁移（000006 冻住，先例 000009 也是「改约束」而非改建表）。
--       值集取「全仓实际写入方逐字集合 + 测试夹具既有取值」，见下方四值注释；
--       应用层同步收口（model.ValidOwnerType + presign/presigner 校验）在同一次提交里做，
--       目的是让未知取值回 400，而不是穿过校验后由本约束在 INSERT 时报 23514、被 handler 兜成 500。
-- 不扩面：OwnerInTeam（repo/team_scope_t378.go:114-123）的 default 分支保持「非患者材料放行」——
--       'ReviewTemplate' 与 'install_record' 正要走这一支；把 default 改成拒会直接打断模板上传链路，
--       且会与列表侧 teamScopedCond 的「NOT IN 患者材料即放行」分叉成两套口径（该文件 :59-60 已自陈此坑）。
--       本约束生效后，未知 owner_type 已不可能进库，default 分支的可达面随之收窄到两个已知非患者材料值。
-- owner：files 表属 file-service（presign 是其唯一写入方；seed.sql 无 files 行）。

BEGIN;

ALTER TABLE files ADD CONSTRAINT files_owner_type_check CHECK (
    owner_type IN (
        'patient',        -- 患者材料。写侧两路：admin-web 复查报告页 review-records/index.vue:159；非 staff 一律由服务端强制成该值（file_handler.go:149）
        'alert',          -- 告警附件。admin-web 告警流程页 FlowRuntime.vue:399
        'ReviewTemplate', -- 复查模板（配置类材料，非患者数据）。file-service 常量 service/presigner.go:42 + review-templates/index.vue:142
        'install_record'  -- 安装记录材料。今日无前端写入方（技师端签名走 install_records.signature_url，不经 files 表），
                          -- 但它是 T022 起 file-service 全套夹具与集成测写入 files 表的取值（repo/pg_store_integration_test.go:94），
                          -- 且 presigner 的 object key 首段就是 owner_type（service/presigner.go:263）——
                          -- 收口面把它留在枚举内，是为了不把别的卡的判据一并改写（卡面红线：禁扩面）
    )
);

COMMENT ON CONSTRAINT files_owner_type_check ON files IS
    'T396：owner_type 枚举收口（第二十四条裁定 a）。值集 = 全仓实际写入方逐字集合 + file-service 夹具既有取值。'
    '与 model.ValidOwnerType 同一集合，改一处必改另一处（有跨层一致性用例钉住）。大小写敏感：历史脏值 Patient / review 不在集合内。';

COMMIT;
