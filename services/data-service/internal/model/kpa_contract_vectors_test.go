package model

import (
	"encoding/json"
	"os"
	"testing"
)

func TestKpaFromNContractVectors(t *testing.T) {
	raw, err := os.ReadFile("../../../../packages/constants/testdata/pressure-unit-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		NToKpa []struct {
			N       float64 `json:"n"`
			AreaCm2 float64 `json:"area_cm2"`
			Kpa     int     `json:"kpa"`
		} `json:"n_to_kpa"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.NToKpa) == 0 {
		t.Fatal("empty fixture")
	}
	for _, c := range v.NToKpa {
		area := c.AreaCm2
		got := KpaFromN(c.N, &area)
		if got == nil || *got != c.Kpa {
			t.Fatalf("KpaFromN(%v,%v)=%v want %d", c.N, c.AreaCm2, got, c.Kpa)
		}
	}
}
