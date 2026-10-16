-- T705 治本：补建 pressure_records 当月起未来 2 个月分区（幂等）
-- 背景（2026-10-17 首证实测）：prod 首发后分区只有 000001 固化的 202607/08/09，
--   预建作业（data-service PARTITION_CRON，默认每月 25 日 0 点）只预建「未来月」
--   （partition.go i=1..2）且当月从未被任何路径补建 ⇒ 10 月上报落库路由
--   pressure_records_202610 缺表，ErrNoPartition 被映射成 20402（与时间超窗同码），
--   首证帧两发全拒（T705 实录）。staging 因 25 日后跑过作业而有 202610/202611。
-- 本迁移：执行时点以 DB 当前月（UTC，与 partition_repo 分区边界 FROM/TO 的
--   UTC 月切语义一致）为起点，补 当月+1+2 共 3 个月，to_regclass 守卫幂等。
-- 为什么用 DO 动态块：迁移文件静态写死月份会在未来月份失效（每月都要再加一次），
--   动态取当前月一次跑、永续覆盖首发/灾备重建/新客户库三种全新库场景。
-- 配套：partition.go 预建循环 i=1..2 → i=0..2（覆盖当月），调度仍 0 0 25 * *。
-- down=单向（注释步）：DROP 分区即删该月生产数据，不可作为回滚步；
--   需要手工移除时按备注命令单独授权执行。
DO $$
DECLARE
    base   date := date_trunc('month', current_date)::date;  -- 当月 1 号（DB 时区=UTC）
    startd date;
    endd   date;
    ym     text;
BEGIN
    FOR i IN 0..2 LOOP
        startd := base + make_interval(months => i);
        endd   := startd + interval '1 month';
        ym     := to_char(startd, 'YYYYMM');
        IF to_regclass('public.pressure_records_' || ym) IS NULL THEN
            EXECUTE format(
                'CREATE TABLE pressure_records_%s PARTITION OF pressure_records FOR VALUES FROM (%L) TO (%L)',
                ym, startd, endd
            );
        END IF;
    END LOOP;
END $$;
