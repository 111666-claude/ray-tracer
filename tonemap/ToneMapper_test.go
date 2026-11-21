package tonemap

import (
	"math"
	"ray-tracer/color"
	"testing"
)

// hdrRamp is a fixed HDR color ramp spanning deep shadows, ordinary
// display-range values and bright highlights well above 1. It is the
// reference input for monotonicity and compression checks.
var hdrRamp = []float64{
	0.0,
	1e-6,
	0.01,
	0.1,
	0.25,
	0.5,
	0.9,
	1.0,
	2.0,
	4.0,
	8.0,
	16.0,
	64.0,
	256.0,
	1024.0,
}

func TestCurvesAreMonotonicAndBounded(t *testing.T) {
	curves := []Curve{LinearCurve, ReinhardCurve, ACESCurve}

	for _, curve := range curves {
		t.Run(curve.String(), func(t *testing.T) {
			mapper := NewToneMapper(0, curve)
			previous := 0.0

			for _, input := range hdrRamp {
				got := mapper.MapChannel(input)

				if math.IsNaN(got) || got < 0 || got > 1 {
					t.Fatalf("MapChannel(%g) = %g, want finite value in [0,1]", input, got)
				}

				if got < previous {
					t.Fatalf("curve %s is not monotonic at %g: %g < previous %g", curve, input, got, previous)
				}

				previous = got
			}
		})
	}
}

func TestZeroAndBlackStayBlack(t *testing.T) {
	for _, curve := range []Curve{LinearCurve, ReinhardCurve, ACESCurve} {
		mapper := NewToneMapper(0, curve)

		if got := mapper.MapChannel(0); got != 0 {
			t.Errorf("curve %s: MapChannel(0) = %g, want 0", curve, got)
		}
	}

	black := NewToneMapper(0, ACESCurve).MapColor(color.Black())
	if black.R != 0 || black.G != 0 || black.B != 0 {
		t.Errorf("black mapped to %v, want {0 0 0}", black)
	}
}

func TestSmoothCurvesCompressHighlights(t *testing.T) {
	// Without compression a value above 1 clips flat immediately. The
	// smooth curves must keep headroom so neighboring bright inputs stay
	// distinguishable (the whole point of highlight compression).
	linear := NewToneMapper(0, LinearCurve)
	if got := linear.MapChannel(16); got != 1 {
		t.Errorf("linear MapChannel(16) = %g, want hard clip to 1", got)
	}

	for _, curve := range []Curve{ReinhardCurve, ACESCurve} {
		mapper := NewToneMapper(0, curve)

		// Values in the shoulder region where the curves retain headroom.
		shoulder := mapper.MapChannel(2)
		brighter := mapper.MapChannel(4)
		brighterStill := mapper.MapChannel(8)

		if shoulder >= 1 {
			t.Errorf("%s MapChannel(2) = %g, want retained highlight headroom", curve, shoulder)
		}

		if !(brighter > shoulder) {
			t.Errorf("%s loses monotonicity in highlights: map(4)=%g <= map(2)=%g", curve, brighter, shoulder)
		}

		if !(brighterStill >= brighter) {
			t.Errorf("%s not monotonic: map(8)=%g < map(4)=%g", curve, brighterStill, brighter)
		}

		// The extreme tail stays finite and in range; ACES saturates to
		// exactly 1 while Reinhard approaches 1 asymptotically.
		if got := mapper.MapChannel(1024); math.IsNaN(got) || got <= 0 || got > 1 {
			t.Errorf("%s MapChannel(1024) = %g, want bounded tail near 1", curve, got)
		}
	}

	// Reinhard keeps headroom for every finite input and approaches 1.
	reinhard := NewToneMapper(0, ReinhardCurve)
	if got := reinhard.MapChannel(16); got >= 1 {
		t.Errorf("Reinhard MapChannel(16) = %g, must stay below 1 for finite input", got)
	}

	if got := reinhard.MapChannel(1024); !(got > 0.99 && got < 1) {
		t.Errorf("Reinhard MapChannel(1024) = %g, want value just below 1", got)
	}

	// ACES saturates fully in its far tail.
	aces := NewToneMapper(0, ACESCurve)
	if got := aces.MapChannel(1024); got != 1 {
		t.Errorf("ACES MapChannel(1024) = %g, want saturated 1", got)
	}
}

func TestExposureScalesInStops(t *testing.T) {
	// Use the linear curve so exposure effects are not masked by the
	// shoulder. +1 EV doubles radiance, -1 EV halves it; on the linear
	// curve the mapped values follow the same ratio.
	base := NewToneMapper(0, LinearCurve).MapChannel(0.5)
	plusOne := NewToneMapper(1, LinearCurve).MapChannel(0.5)
	minusOne := NewToneMapper(-1, LinearCurve).MapChannel(0.5)

	if math.Abs(plusOne/base-2) > 1e-12 {
		t.Errorf("+1 EV gave ratio %g, want 2", plusOne/base)
	}

	if math.Abs(minusOne/base-0.5) > 1e-12 {
		t.Errorf("-1 EV gave ratio %g, want 0.5", minusOne/base)
	}
}

func TestNegativeExposureKeepsShadowsMonotonic(t *testing.T) {
	mapper := NewToneMapper(-3, ACESCurve)
	previous := 0.0

	for _, input := range hdrRamp {
		got := mapper.MapChannel(input)
		if got < previous {
			t.Fatalf("negative exposure broke monotonicity at %g: %g < %g", input, got, previous)
		}

		previous = got
	}

	// A strongly negative exposure must map ordinary radiance into the
	// dark end of the range instead of washing it out.
	if got := mapper.MapChannel(1); got > 0.25 {
		t.Errorf("EV -3 MapChannel(1) = %g, expected dark value", got)
	}
}

func TestExposureBrightensWholeRamp(t *testing.T) {
	// For any fixed finite input, adding exposure must never darken it.
	for _, curve := range []Curve{ReinhardCurve, ACESCurve} {
		dark := NewToneMapper(-2, curve)
		bright := NewToneMapper(2, curve)

		for _, input := range hdrRamp[1:] {
			if bright.MapChannel(input) < dark.MapChannel(input) {
				t.Fatalf("curve %s: +2 EV darker than -2 EV at input %g", curve, input)
			}
		}
	}
}

func TestNonFiniteAndNegativeInputs(t *testing.T) {
	mapper := NewToneMapper(0, ACESCurve)

	cases := []struct {
		name string
		in   float64
		want float64
	}{
		{"NaN", math.NaN(), 0},
		{"positive infinity", math.Inf(1), 1},
		{"negative infinity", math.Inf(-1), 0},
		{"negative finite", -42, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mapper.MapChannel(tc.in); got != tc.want {
				t.Errorf("MapChannel(%s) = %g, want %g", tc.name, got, tc.want)
			}
		})
	}

	// NaN exposure must not poison pixels: it behaves as zero exposure.
	nanExposure := NewToneMapper(math.NaN(), ACESCurve)
	if got := nanExposure.MapChannel(0.5); !(got > 0 && got < 1) {
		t.Errorf("NaN exposure produced out-of-range value %g", got)
	}

	// +Inf combined with a finite value saturates to white rather than
	// propagating NaN through the curve.
	if got := mapper.MapChannel(math.Inf(1)); got != 1 {
		t.Errorf("+Inf mapped to %g, want 1", got)
	}
}

func TestMapColorHandlesMixedChannels(t *testing.T) {
	mapper := NewToneMapper(0, ReinhardCurve)
	mapped := mapper.MapColor(color.NewColor(math.NaN(), 0, math.Inf(1)))

	if mapped.R != 0 {
		t.Errorf("NaN channel = %g, want 0", mapped.R)
	}

	if mapped.G != 0 {
		t.Errorf("zero channel = %g, want 0", mapped.G)
	}

	if mapped.B != 1 {
		t.Errorf("+Inf channel = %g, want 1", mapped.B)
	}
}

func TestCurveString(t *testing.T) {
	if got := LinearCurve.String(); got != "linear" {
		t.Errorf("LinearCurve.String() = %q", got)
	}

	if got := ReinhardCurve.String(); got != "reinhard" {
		t.Errorf("ReinhardCurve.String() = %q", got)
	}

	if got := ACESCurve.String(); got != "aces" {
		t.Errorf("ACESCurve.String() = %q", got)
	}
}

func TestDefaultToneMapper(t *testing.T) {
	mapper := DefaultToneMapper()
	if mapper.Curve != ACESCurve || mapper.Exposure != 0 {
		t.Errorf("DefaultToneMapper = %+v, want ACES at EV 0", mapper)
	}
}
