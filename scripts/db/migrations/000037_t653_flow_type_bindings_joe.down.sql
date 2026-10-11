-- T653 回滚：整表删除。绑定为配置层，无独立业务数据需要保留；恢复后所有类型回到「未绑定」语义（不自动建实例）。
BEGIN;

DROP TABLE IF EXISTS flow_type_bindings;

COMMIT;
