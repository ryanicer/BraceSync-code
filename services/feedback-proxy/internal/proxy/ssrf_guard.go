package proxy

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// validateTapdBase 出网地址白名单闸（防 SSRF：base 虽来自 env，仍按宿主/方案/私网三重校验，
// 只放 https + tapd.cn 系域名；环回/链路本地/私网/元数据地址一律拒绝）。
// 联调单测可用 https://127.0.0.1（仅当 FEEDBACK_ALLOW_LOOPBACK=1 的测试路径，见 NewHandler）。
func validateTapdBase(raw string, allowLoopback bool) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("tapd base: parse: %w", err)
	}
	if u.Scheme != "https" && !allowLoopback {
		return fmt.Errorf("tapd base: scheme must be https")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("tapd base: empty host")
	}
	if allowLoopback {
		return nil // 仅单测路径（httptest 是 http://127.0.0.1）
	}
	if !strings.HasSuffix(host, "tapd.cn") {
		return fmt.Errorf("tapd base: host not in tapd.cn allowlist")
	}
	if ip := net.ParseIP(host); ip != nil && isPrivateIP(ip) {
		return fmt.Errorf("tapd base: private ip blocked")
	}
	return nil
}

// isPrivateIP 环回/链路本地/私网/CGNAT/组播/未指定——全部当红。
func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	// 100.64/10（CGNAT，含部分云元数据面）
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
		return true
	}
	// 169.254.169.254 类元数据地址已被 link-local 覆盖；此处再挡 192.0.0.0/24 与 198.18/15
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 192 && v4[1] == 0 && v4[2] == 0 {
			return true
		}
		if v4[0] == 198 && (v4[1] == 18 || v4[1] == 19) {
			return true
		}
	}
	return false
}
