// Package tonemap converts high dynamic range (HDR) linear radiance values
// into the [0, 1] display range.
//
// The pipeline works entirely in linear space: the renderer accumulates
// linear radiance, a ToneMapper applies an exposure scale followed by a
// highlight compression curve, and only afterwards is the result gamma
// encoded by the output writer. This keeps highlight detail around bright
// lights instead of hard-clipping it to white, while negative exposure
// values preserve shadow detail without washing the whole frame out.
package tonemap

import (
	"fmt"
	"math"
	"ray-tracer/color"
)

// Curve selects the highlight compression curve applied after exposure.
type Curve int

const (
	// LinearCurve applies no compression: values are hard-clipped to the
	// display range. Reproduces the legacy output behaviour.
	LinearCurve Curve = iota

	// ReinhardCurve is the smooth Reinhard shoulder x/(1+x). It is
	// monotonically increasing, maps 0 to 0 and asymptotically approaches
	// 1 for bright highlights, so no finite radiance is ever clipped flat.
	ReinhardCurve

	// ACESCurve is the smooth Narkowicz ACES filmic approximation. It has
	// a gentle toe in the shadows and a soft shoulder in the highlights.
	ACESCurve
)

// String returns the human readable name of the curve.
func (c Curve) String() string {
	switch c {
	case LinearCurve:
		return "linear"
	case ReinhardCurve:
		return "reinhard"
	case ACESCurve:
		return "aces"
	default:
		return fmt.Sprintf("unknown(%d)", int(c))
	}
}

// ToneMapper maps linear HDR radiance to display-ready linear values in
// [0, 1]. The zero value is not valid; use NewToneMapper.
type ToneMapper struct {
	// Exposure is the exposure adjustment in photographic stops (EV).
	// Positive values brighten the image, negative values darken it.
	Exposure float64

	// Curve is the highlight compression curve applied after exposure.
	Curve Curve
}

// NewToneMapper returns a ToneMapper using the given exposure (in EV stops)
// and compression curve.
func NewToneMapper(exposure float64, curve Curve) *ToneMapper {
	return &ToneMapper{
		Exposure: exposure,
		Curve:    curve,
	}
}

// DefaultToneMapper returns a tone mapper with zero exposure and the ACES
// filmic curve, which keeps both shadow and highlight detail for typical
// HDR scenes.
func DefaultToneMapper() *ToneMapper {
	return NewToneMapper(0, ACESCurve)
}

// MapColor applies the tone mapper to every channel of a linear HDR color.
// Non-finite inputs are sanitized to a defined result: NaN becomes black
// and +Inf becomes white.
func (m *ToneMapper) MapColor(input *color.Color) *color.Color {
	return color.NewColor(
		m.MapChannel(input.R),
		m.MapChannel(input.G),
		m.MapChannel(input.B),
	)
}

// MapChannel maps a single linear HDR radiance value to a display value in
// [0, 1]. NaN maps to 0, negative radiance is treated as 0 and +Inf maps
// to 1 regardless of the configured curve.
func (m *ToneMapper) MapChannel(value float64) float64 {
	if math.IsNaN(value) {
		return 0
	}

	// A NaN exposure would poison every pixel; treat it as "no adjustment".
	exposure := m.Exposure
	if math.IsNaN(exposure) {
		exposure = 0
	}

	scaled := value * math.Pow(2, exposure)

	if math.IsNaN(scaled) {
		return 0
	}

	if math.IsInf(scaled, 1) {
		return 1
	}

	if scaled <= 0 {
		return 0
	}

	mapped := m.compress(scaled)

	// Defensive clamp: every shipped curve is contractually bounded, but
	// this also guards custom callers computing their own curves.
	if mapped < 0 {
		return 0
	}

	if mapped > 1 || math.IsNaN(mapped) {
		return 1
	}

	return mapped
}

// compress applies the configured highlight compression curve to a value
// that is guaranteed finite and strictly positive.
func (m *ToneMapper) compress(x float64) float64 {
	switch m.Curve {
	case LinearCurve:
		// No compression: the caller's final clamp performs the hard clip.
		return x
	case ReinhardCurve:
		return reinhard(x)
	case ACESCurve:
		return aces(x)
	default:
		return aces(x)
	}
}

// reinhard is the classic x/(1+x) shoulder. Monotonic and bounded by 1.
func reinhard(x float64) float64 {
	return x / (1 + x)
}

// aces evaluates the Narkowicz ACES filmic mapping approximation. The
// polynomial is monotonic for x >= 0, starts at 0 and saturates near 1.
func aces(x float64) float64 {
	const (
		a = 2.51
		b = 0.03
		c = 2.43
		d = 0.59
		e = 0.14
	)

	numerator := x * (a*x + b)
	denominator := x*(c*x+d) + e

	return numerator / denominator
}
