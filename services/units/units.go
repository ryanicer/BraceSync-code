// Package units 是全仓 Go 侧唯一的压力量纲换算真源。
// 存储/传输/聚合/告警判档/校准减除全程 mN，零换算；N 只允许出现在 API 出口。
package units

// MnPerN 毫牛 mN 到牛 N 的唯一换算分母。
const MnPerN = 1000.0

// MnToN 存储空间 mN 到展示空间 N；负值原样保留。
func MnToN(mn float64) float64 { return mn / MnPerN }

// NToMn 展示空间 N 到存储空间 mN。
func NToMn(n float64) float64 { return n * MnPerN }

// MnStampToN 指针版 mN 到 N；nil 原样传 nil，不退 0。
func MnStampToN(v *float64) *float64 {
	if v == nil {
		return nil
	}
	n := MnToN(*v)
	return &n
}

// NStampToMn 指针版 N 到 mN；nil 原样传 nil，不退 0。
func NStampToMn(v *float64) *float64 {
	if v == nil {
		return nil
	}
	mn := NToMn(*v)
	return &mn
}
