package core_test

import (
	"bytes"
	"errors"
	"image/png"
	"math"
	"ray-tracer/color"
	"ray-tracer/core"
	"ray-tracer/material"
	"ray-tracer/object"
	"ray-tracer/random"
	"ray-tracer/tonemap"
	"ray-tracer/vector"
	"ray-tracer/writer"
	"strconv"
	"strings"
	"testing"
)

const testSeed uint64 = 20261122

// newTestCamera builds a small deterministic camera aimed at a scene with
// a very bright HDR light source, so both shadow detail and highlight
// compression are exercised.
func newTestCamera() *core.Camera {
	camera := core.NewCamera()
	camera.AspectRatio = 1
	camera.ImageWidth = 24
	camera.SamplesPerPixel = 8
	camera.MaxDepth = 6
	camera.Background = color.NewColor(0.02, 0.03, 0.05)
	camera.VerticalFieldOfView = 40
	camera.LookFrom = vector.NewPoint3(0, 1, -6)
	camera.LookAt = vector.NewPoint3(0, 1, 0)
	camera.VectorUp = vector.NewVector3(0, 1, 0)
	camera.DefocusAngle = 0
	camera.FocusDistance = 10

	return camera
}

// newTestWorld contains an emissive sphere (radiance far above 1), a
// matte sphere in its vicinity and a large ground plane sphere.
func newTestWorld() core.Hittable {
	world := core.EmptyHittableList()

	ground := material.NewLambertian(color.NewColor(0.5, 0.5, 0.5))
	matte := material.NewLambertian(color.NewColor(0.9, 0.2, 0.2))
	light := material.NewDiffuseLight(color.NewColor(40, 40, 40))

	world.Add(object.NewSphere(vector.NewPoint3(0, -100.5, 1), 100, ground))
	world.Add(object.NewSphere(vector.NewPoint3(-1.2, 0.7, 1.2), 0.7, matte))
	world.Add(object.NewSphere(vector.NewPoint3(1.4, 1.2, 0.6), 1.2, light))

	return world
}

// render runs one fully deterministic render and returns the encoded bytes.
func render(t *testing.T, format writer.Format, exposure float64, curve tonemap.Curve) []byte {
	t.Helper()

	random.Seed(testSeed, random.DefaultPCGSeed2)

	var buf bytes.Buffer
	w := writer.NewWriter("",
		writer.WithOutput(&buf),
		writer.WithFormat(format),
		writer.WithToneMapper(tonemap.NewToneMapper(exposure, curve)),
	)

	camera := newTestCamera()
	if err := camera.Render(newTestWorld(), w); err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	return buf.Bytes()
}

func decodePPMBytes(t *testing.T, data []byte) [][3]uint8 {
	t.Helper()

	fields := strings.Fields(string(data))
	if fields[0] != "P3" {
		t.Fatalf("not a P3 PPM")
	}

	width, _ := strconv.Atoi(fields[1])
	height, _ := strconv.Atoi(fields[2])

	pixels := make([][3]uint8, 0, width*height)

	for i := 4; i+2 < len(fields); i += 3 {
		r, _ := strconv.Atoi(fields[i])
		g, _ := strconv.Atoi(fields[i+1])
		b, _ := strconv.Atoi(fields[i+2])
		pixels = append(pixels, [3]uint8{uint8(r), uint8(g), uint8(b)})
	}

	return pixels
}

func decodePNGBytes(t *testing.T, data []byte) [][3]uint8 {
	t.Helper()

	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("png decode: %v", err)
	}

	bounds := img.Bounds()
	pixels := make([][3]uint8, 0, bounds.Dx()*bounds.Dy())

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			pixels = append(pixels, [3]uint8{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)})
		}
	}

	return pixels
}

func TestPNGAndPPMMatchForSameSeed(t *testing.T) {
	ppm := decodePPMBytes(t, render(t, writer.PPMFormat, 0, tonemap.ACESCurve))
	pngPixels := decodePNGBytes(t, render(t, writer.PNGFormat, 0, tonemap.ACESCurve))

	if len(ppm) != len(pngPixels) {
		t.Fatalf("pixel count mismatch: ppm=%d png=%d", len(ppm), len(pngPixels))
	}

	differences := 0

	for i := range ppm {
		if ppm[i] != pngPixels[i] {
			differences++
		}
	}

	if differences != 0 {
		t.Fatalf("%d of %d pixels differ between PPM and PNG", differences, len(ppm))
	}
}

func TestSameSeedIsReproducible(t *testing.T) {
	first := render(t, writer.PPMFormat, 0, tonemap.ACESCurve)
	second := render(t, writer.PPMFormat, 0, tonemap.ACESCurve)

	if !bytes.Equal(first, second) {
		t.Fatal("two renders with the same seed produced different output")
	}
}

// averageLuma returns the mean perceptual-ish luma across rendered pixels.
func averageLuma(pixels [][3]uint8) float64 {
	sum := 0.0

	for _, p := range pixels {
		sum += 0.2126*float64(p[0]) + 0.7152*float64(p[1]) + 0.0722*float64(p[2])
	}

	return sum / float64(len(pixels))
}

func TestExposureMonotonicOnScene(t *testing.T) {
	dark := decodePPMBytes(t, render(t, writer.PPMFormat, -2, tonemap.ACESCurve))
	neutral := decodePPMBytes(t, render(t, writer.PPMFormat, 0, tonemap.ACESCurve))
	bright := decodePPMBytes(t, render(t, writer.PPMFormat, 2, tonemap.ACESCurve))

	lumaDark := averageLuma(dark)
	lumaNeutral := averageLuma(neutral)
	lumaBright := averageLuma(bright)

	if !(lumaDark < lumaNeutral && lumaNeutral < lumaBright) {
		t.Fatalf("exposure not monotonic: dark=%g neutral=%g bright=%g", lumaDark, lumaNeutral, lumaBright)
	}

	// Negative exposure must preserve distinct shadow values instead of
	// collapsing everything to pure black.
	nonZeroShadows := 0

	for _, p := range dark {
		if p[0]+p[1]+p[2] > 0 {
			nonZeroShadows++
		}
	}

	if nonZeroShadows == 0 {
		t.Fatal("negative exposure flattened the whole scene to black")
	}
}

func TestFilmicCurvePreservesHighlightDetail(t *testing.T) {
	// The HDR light has radiance 40. A hard linear clip turns every
	// over-bright sample into the same 255, while ACES retains gradation:
	// the brightest pixel region should contain white, but not all bright
	// pixels can be identical.
	linearPixels := decodePPMBytes(t, render(t, writer.PPMFormat, 0, tonemap.LinearCurve))
	acesPixels := decodePPMBytes(t, render(t, writer.PPMFormat, 0, tonemap.ACESCurve))

	distinctLinear := map[[3]uint8]bool{}
	distinctACES := map[[3]uint8]bool{}

	for _, p := range linearPixels {
		if p[0] > 200 && p[1] > 200 && p[2] > 200 {
			distinctLinear[p] = true
		}
	}

	for _, p := range acesPixels {
		if p[0] > 200 && p[1] > 200 && p[2] > 200 {
			distinctACES[p] = true
		}
	}

	if len(distinctACES) == 0 {
		t.Fatal("ACES render contains no bright pixels at all")
	}

	// Hard clipping collapses the bright region to (near-)uniform white;
	// ACES must retain strictly more distinct bright values.
	if len(distinctACES) <= len(distinctLinear) {
		t.Fatalf("ACES did not retain more highlight gradation than hard clip: aces=%d linear=%d",
			len(distinctACES), len(distinctLinear))
	}
}

func TestAllPixelsAreValidBytes(t *testing.T) {
	// Renders at extreme exposures must still emit only valid 8-bit values
	// and never NaN-derived garbage.
	for _, exposure := range []float64{-10, 10} {
		pixels := decodePPMBytes(t, render(t, writer.PPMFormat, exposure, tonemap.ACESCurve))

		for i, p := range pixels {
			for _, channel := range p {
				if channel > 255 {
					t.Fatalf("exposure %g pixel %d out of byte range: %v", exposure, i, p)
				}
			}
		}
	}
}

func TestRenderPropagatesWriteError(t *testing.T) {
	random.Seed(testSeed, random.DefaultPCGSeed2)

	w := writer.NewWriter("",
		writer.WithOutput(failingCoreWriter{}),
		writer.WithFormat(writer.PPMFormat),
	)

	err := newTestCamera().Render(newTestWorld(), w)
	if err == nil {
		t.Fatal("Render did not propagate the write failure")
	}
}

// failingCoreWriter rejects every write to simulate a broken output sink.
type failingCoreWriter struct{}

var errBrokenSink = errors.New("simulated broken output sink")

func (failingCoreWriter) Write(p []byte) (int, error) {
	return 0, errBrokenSink
}

func TestNaNBackgroundRendersAsBlack(t *testing.T) {
	random.Seed(testSeed, random.DefaultPCGSeed2)

	camera := newTestCamera()
	camera.Background = color.NewColor(math.NaN(), math.NaN(), math.NaN())

	world := core.EmptyHittableList() // every ray misses -> background color

	var buf bytes.Buffer
	w := writer.NewWriter("",
		writer.WithOutput(&buf),
		writer.WithFormat(writer.PNGFormat),
		writer.WithToneMapper(tonemap.NewToneMapper(0, tonemap.ACESCurve)),
	)

	if err := camera.Render(world, w); err != nil {
		t.Fatalf("Render: %v", err)
	}

	for _, p := range decodePNGBytes(t, buf.Bytes()) {
		if p != [3]uint8{0, 0, 0} {
			t.Fatalf("NaN background rendered as %v, want black", p)
		}
	}
}
