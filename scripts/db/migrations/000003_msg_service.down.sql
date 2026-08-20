-- 000003_msg_service.down.sql — 回滚 msg-service 通知域表
DROP TABLE IF EXISTS quota_grants;
DROP TABLE IF EXISTS notification_retry_queue;
DROP TABLE IF EXISTS notification_records;
