package color

import "testing"

func TestNewColorAndConstants(t *testing.T) {
	c := NewColor(0.1, 0.2, 0.3)
	if c.R != 0.1 || c.G != 0.2 || c.B != 0.3 {
		t.Fatalf("NewColor = %v", c)
	}

	if Black().R != 0 || Black().G != 0 || Black().B != 0 {
		t.Error("Black is not black")
	}

	if White().R != 1 || White().G != 1 || White().B != 1 {
		t.Error("White components are not 1")
	}
}

func TestColorArithmetic(t *testing.T) {
	a := NewColor(1, 2, 3)
	b := NewColor(4, 5, 6)

	sum := a.Add(b)
	if *sum != (Color{5, 7, 9}) {
		t.Errorf("Add = %v", sum)
	}

	product := a.Multiply(b)
	if *product != (Color{4, 10, 18}) {
		t.Errorf("Multiply = %v", product)
	}

	scaled := a.MultiplyBy(2)
	if *scaled != (Color{2, 4, 6}) {
		t.Errorf("MultiplyBy = %v", scaled)
	}

	target := NewColor(0, 0, 0)
	target.AddInPlace(a)
	if *target != *a {
		t.Errorf("AddInPlace = %v, want %v", target, a)
	}

	clone := NewColor(9, 9, 9)
	clone.CopyFrom(a)
	if *clone != *a {
		t.Errorf("CopyFrom = %v, want %v", clone, a)
	}
}
