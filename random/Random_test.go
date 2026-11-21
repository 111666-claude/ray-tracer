package random

import "testing"

func TestSeedProducesDeterministicStream(t *testing.T) {
	first := make([]float64, 64)

	Seed(12345, DefaultPCGSeed2)

	for i := range first {
		first[i] = UnitFloat64()
	}

	second := make([]float64, 64)

	Seed(12345, DefaultPCGSeed2)

	for i := range second {
		second[i] = UnitFloat64()
	}

	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("re-seeding did not restore the stream at index %d: %g != %g", i, first[i], second[i])
		}
	}
}

func TestDifferentSeedsDiverge(t *testing.T) {
	Seed(1, DefaultPCGSeed2)
	a := UnitFloat64()

	Seed(2, DefaultPCGSeed2)
	b := UnitFloat64()

	if a == b {
		t.Fatal("different seeds produced identical first draws")
	}
}

func TestUnitFloat64StaysInRange(t *testing.T) {
	Seed(DefaultPCGSeed1, DefaultPCGSeed2)

	for range 1000 {
		x := UnitFloat64()
		if x < 0 || x >= 1 {
			t.Fatalf("UnitFloat64 out of [0,1): %g", x)
		}
	}
}

func TestFloat64Interval(t *testing.T) {
	Seed(7, DefaultPCGSeed2)

	for range 1000 {
		x := Float64(-2, 10)
		if x < -2 || x >= 10 {
			t.Fatalf("Float64(-2,10) out of range: %g", x)
		}
	}
}

func TestInt32NBounds(t *testing.T) {
	Seed(7, DefaultPCGSeed2)

	for range 1000 {
		n := Int32N(4)
		if n < 0 || n >= 4 {
			t.Fatalf("Int32N(4) out of range: %d", n)
		}
	}
}
