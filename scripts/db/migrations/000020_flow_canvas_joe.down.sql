-- T274 回滚：flow_* 四表。DROP 顺序与依赖相反（子表先、父表后）；
-- flow_instance 上的两个子表带 ON DELETE CASCADE，但 DROP TABLE 不级联删表，必须逐条 DROP。
BEGIN;

DROP TABLE IF EXISTS flow_node_action;
DROP TABLE IF EXISTS flow_node_state;
DROP TABLE IF EXISTS flow_instance;
DROP TABLE IF EXISTS flow_template;

COMMIT;
