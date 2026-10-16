package proxy

import (
	"fmt"
	"strings"
)

// bugTitle 建单标题统一前缀（D-1/D-5：[运维反馈] 便于列表识别与搜索）。
func bugTitle(title string) string {
	return "[运维反馈] " + title
}

// describeFirstLine 建单时描述首行的占位（受理号要等 bug id 才能算，D-2 步2→3）；
// 拿到 id 后由 replaceReceipt 回填成最终受理号行。
const describeFirstLine = "受理号 生成中（本单由内部反馈通道 API 代建）"

// buildDescription 按 D-5 固定模板拼装描述：受理号首行 + 提交人/环境/分类/正文/路径。
func buildDescription(r submitReq, firstLine string) string {
	var b strings.Builder
	b.WriteString(firstLine + "\n")
	b.WriteString("提交人 " + r.Submitter + "（运维侧）\n")
	b.WriteString("提交环境（系统自动识别）\n")
	b.WriteString(envLabel(r.Env) + "\n")
	b.WriteString("问题分类 分类:" + r.Category + "\n")
	b.WriteString("问题描述\n")
	b.WriteString(r.Description + "\n")
	if strings.TrimSpace(r.PagePath) != "" {
		b.WriteString("出错页面路径 " + r.PagePath + "\n")
	}
	return b.String()
}

// replaceReceipt 把占位首行换成受理号首行（D-2 步3：受理号写入缺陷描述首段）。
func replaceReceipt(desc, receipt string) string {
	line := fmt.Sprintf("受理号 %s（凭此号对应运维侧回执；本单由内部反馈通道 API 代建）", receipt)
	idx := strings.IndexByte(desc, '\n')
	if idx < 0 {
		return line
	}
	return line + desc[idx:]
}
