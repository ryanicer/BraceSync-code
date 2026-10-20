// Package phone T727：查重哈希口径切换的前置不变量。
//
// T727 把服务端写入口的 phone_hash 口径由 Hash(plain) 改为 NormalizeHash(plain)。
// 这个改动「不影响现网任何一行」的依据不是运气，而是下面两条一起成立的性质，
// 所以它们必须有测试钉住 —— 一旦哪天有人放宽 validPhone 或改动 Normalize，
// 断掉的是全仓患者/技师/医护的手机号查重键与登录查找键。
package phone

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// isValidShape 复刻 handler.validPhone：11 位、1 开头、纯数字。
// 写入口全部先过这道门（handler.go createPatient/updatePatientPhone/createTechnician/
// updateTechnician/doctor_accounts_t314），所以这条测试覆盖的输入集就是真能落库的输入集。
func isValidShape(p string) bool {
	if len(p) != 11 || p[0] != '1' {
		return false
	}
	for _, ch := range p {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

// TestNormalizeHashIdentityOnValidPhone 合法入参下两种口径逐字节同值 ⇒ 历史 phone_hash 无需回填。
func TestNormalizeHashIdentityOnValidPhone(t *testing.T) {
	cases := []string{
		"13800138000", "10000000000", "19999999999", "13012345678",
		"18811112222", "17777777777", "15500001234", "12345678901",
	}
	// 号段遍历 + 定长尾号，覆盖所有 1 开头第 2 位取值（不做随机，读数要可复算）
	for second := 0; second <= 9; second++ {
		for tail := 0; tail < 100; tail++ {
			cases = append(cases, string([]rune{'1', rune('0' + second)})+"38000"+pad4(tail))
		}
	}

	for _, p := range cases {
		if !isValidShape(p) {
			t.Fatalf("用例本身须过 validPhone：%q", p)
		}
		assert.Equal(t, Hash(p), NormalizeHash(p),
			"Normalize 对合法手机号必须是恒等变换（口径切换零失配的前提）")
	}
}

// TestNormalizeHashDivergesOnDirtyInput 反向证据：脏输入下两口径确实不同值。
// 这正是本次统一口径要防的分叉 —— 微信 bind-phone（NormalizeHash）与后台建档（原 Hash）
// 若各自为政，同一个号的两个写入路径会落进两个不同的 phone_hash，
// 唯一索引与查重双双失效，登录侧则按其中一把找档案（10-20 事故的机制候选）。
func TestNormalizeHashDivergesOnDirtyInput(t *testing.T) {
	const clean = "13800138000"
	dirty := []string{"+8613800138000", "8613800138000", " 13800138000", "138 0013 8000", "\u200b13800138000"}
	for _, d := range dirty {
		assert.NotEqual(t, Hash(d), Hash(clean), "前提：脏串的裸哈希与干净号不同键")
		assert.Equal(t, NormalizeHash(clean), NormalizeHash(d),
			"规范化后须收口到同一把键：%q", d)
	}
}

func pad4(n int) string {
	d := [4]byte{'0', '0', '0', '0'}
	for i := 3; i >= 0 && n > 0; i-- {
		d[i] = byte('0' + n%10)
		n /= 10
	}
	return string(d[:])
}
