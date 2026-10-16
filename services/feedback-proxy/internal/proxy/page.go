package proxy

import _ "embed"

//go:embed static/index.html
var pageHTML []byte

// Page 静态提交页（01 屏，Boss 批 peter v3 设计稿的可交互实现）。
func Page() []byte { return pageHTML }
