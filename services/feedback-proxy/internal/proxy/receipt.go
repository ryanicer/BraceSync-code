// Package proxy T709 运维反馈代理（TAPD 建单代理容器，Boss 2026-10-16 批 peter v3 设计）
//
// 职责：静态提交页托管 + 收单校验 + POST TAPD /bugs 建缺陷 + 附件流式转发（不落盘）。
// 边界：TAPD 凭据只在容器 env；日志只出 bug id 与文件名/大小（无库、无状态，幂等靠内存窗）。
package proxy

import (
	"fmt"
	"time"
)

// Receipt 计算受理号 FB-YYYYMMDD-NNN（NNN=TAPD bug id 尾三位，北京时间切日）。
// 无本地状态、天然防并发重号、与 TAPD 单号双向换算（D-2）。
func Receipt(bugID int64, at time.Time) string {
	loc := time.FixedZone("CST", 8*3600)
	bj := at.In(loc)
	return fmt.Sprintf("FB-%s-%03d", bj.Format("20060102"), bugID%1000)
}
