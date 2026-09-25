-- T396 down：撤掉 files_owner_type_check，列回到 000006 建表时的「无枚举约束」状态
-- 不删数据：约束本身不改写任何行，回滚只需 DROP CONSTRAINT；
-- ⚠ 回滚后「未知 owner_type 可入库」这一格重新打开（T378 遗留第 2 条的 fail-open 面回来），
--    应用层的 model.ValidOwnerType 校验仍在（它独立生效，与库约束互为两道门），
--    届时「过得了应用层四值、却因别的写入方塞进脏值」的库行会重新出现。
-- 反向注意：若库里已存在集合外取值（例如有人在 staging 手工 INSERT 过大写 'Patient'），
--    up 会因 23514 校验存量而整事务失败——这是刻意的：宁可迁移报错，
--    也不要 ADD CONSTRAINT NOT VALID 把脏值留在库里（那等于只挡新数据、装作已收口）。
--    真要带脏值上线，先按 T391 取证口径清洗，再跑 up（本轮 staging 已由 PM 删净 3 行）。

BEGIN;

ALTER TABLE files DROP CONSTRAINT IF EXISTS files_owner_type_check;

COMMIT;
