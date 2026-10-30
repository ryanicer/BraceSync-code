package units

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

type unitVectors struct {
	MnToN []struct {
		Mn float64 `json:"mn"`
		N  float64 `json:"n"`
	} `json:"mn_to_n"`
}

func TestMnToNContractVectors(t *testing.T) {
	raw, err := os.ReadFile("../../packages/constants/testdata/pressure-unit-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v unitVectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.MnToN) == 0 {
		t.Fatal("empty fixture")
	}
	for _, c := range v.MnToN {
		if got := MnToN(c.Mn); math.Abs(got-c.N) > 1e-12 {
			t.Fatalf("MnToN(%v)=%v want %v", c.Mn, got, c.N)
		}
	}
}
