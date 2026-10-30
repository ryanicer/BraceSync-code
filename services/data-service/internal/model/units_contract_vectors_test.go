package model

import (
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/bracesync/bracesync/services/units"
)

// T755 备用锁值腿：CI-Go 若未把 ./services/units/... 加入枚举，
// 本测试仍随 data-service（已枚举）运行，保证 mN ↔ N 向量在 CI 有闸。
func TestUnitsMnToNContractVectors(t *testing.T) {
	raw, err := os.ReadFile("../../../../packages/constants/testdata/pressure-unit-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		MnToN []struct {
			Mn float64 `json:"mn"`
			N  float64 `json:"n"`
		} `json:"mn_to_n"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.MnToN) == 0 {
		t.Fatal("empty fixture")
	}
	for _, c := range v.MnToN {
		if got := units.MnToN(c.Mn); math.Abs(got-c.N) > 1e-12 {
			t.Fatalf("MnToN(%v)=%v want %v", c.Mn, got, c.N)
		}
		if got := units.NToMn(c.N); math.Abs(got-c.Mn) > 1e-9 {
			t.Fatalf("NToMn(%v)=%v want %v", c.N, got, c.Mn)
		}
	}
}
