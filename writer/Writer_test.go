package writer

import (
	"bytes"
	"errors"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"ray-tracer/color"
	"ray-tracer/tonemap"
	"strings"
	"testing"
)

// pixels is a fixed set of linear HDR pixels covering shadows, display
// range, highlights and non-finite values.
func testPixels() []*color.Color {
	return []*color.Color{
		color.NewColor(0, 0, 0),
		color.NewColor(0.001, 0.01, 0.1),
		color.NewColor(0.5, 0.5, 0.5),
		color.NewColor(1, 1, 1),
		color.NewColor(4, 8, 16),
		color.NewColor(100, 1000, 10000),
		color.NewColor(math.NaN(), math.Inf(1), -5),
	}
}

// failingWriter fails every Write after the first failAfter successful
// bytes, modeling an interrupted write (broken pipe, full disk, ...).
type failingWriter struct {
	buf       bytes.Buffer
	failAfter int
	written   int
}

func (f *failingWriter) Write(p []byte) (int, error) {
	if f.written >= f.failAfter {
		return 0, errors.New("simulated write interruption")
	}

	limit := min(len(p), f.failAfter-f.written)
	n, err := f.buf.Write(p[:limit])
	f.written += n

	if err != nil {
		return n, err
	}

	if limit < len(p) {
		return n, errors.New("simulated write interruption")
	}

	return n, nil
}

// renderInto writes the test pixels through a fully configured writer.
func renderInto(t *testing.T, output io.Writer, format Format, tm *tonemap.ToneMapper) *Writer {
	t.Helper()

	pixels := testPixels()
	w := NewWriter("", WithOutput(output), WithFormat(format), WithToneMapper(tm))

	if err := w.WriteHeader(len(pixels), 1); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}

	for _, pixel := range pixels {
		if err := w.WriteColor(pixel); err != nil {
			t.Fatalf("WriteColor: %v", err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	return w
}

func TestFormatFromName(t *testing.T) {
	cases := map[string]Format{
		"out.png":      PNGFormat,
		"out.PNG":      PNGFormat,
		"dir/out.ppm":  PPMFormat,
		"out.pnm":      PPMFormat,
		"out.txt":      PPMFormat,
		"out":          PPMFormat,
		"out.jpeg":     PPMFormat,
		"weird.png.bk": PPMFormat,
	}

	for name, want := range cases {
		if got := formatFromName(name); got != want {
			t.Errorf("formatFromName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestFormatString(t *testing.T) {
	if PPMFormat.String() != "ppm" || PNGFormat.String() != "png" {
		t.Fatalf("unexpected format names: %q, %q", PPMFormat, PNGFormat)
	}
}

// decodePPM parses a P3 PPM into raw 8-bit RGB triples.
func decodePPM(t *testing.T, data []byte) [][3]uint8 {
	t.Helper()

	parts := strings.Fields(string(data))
	if len(parts) < 4 || parts[0] != "P3" {
		t.Fatalf("not a P3 PPM: %q", data[:min(len(data), 16)])
	}

	width := atoi(t, parts[1])
	height := atoi(t, parts[2])
	maxValue := atoi(t, parts[3])

	if maxValue != 255 {
		t.Fatalf("unexpected max value %d", maxValue)
	}

	pixels := make([][3]uint8, 0, width*height)

	for i := 4; i+2 < len(parts); i += 3 {
		pixels = append(pixels, [3]uint8{
			uint8(atoi(t, parts[i])),
			uint8(atoi(t, parts[i+1])),
			uint8(atoi(t, parts[i+2])),
		})
	}

	if len(pixels) != width*height {
		t.Fatalf("decoded %d pixels, header declares %d", len(pixels), width*height)
	}

	return pixels
}

func atoi(t *testing.T, s string) int {
	t.Helper()

	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			t.Fatalf("non-numeric PPM token %q", s)
		}

		n = n*10 + int(r-'0')
	}

	return n
}

// decodePNG parses an 8-bit PNG into raw RGB triples.
func decodePNG(t *testing.T, data []byte) [][3]uint8 {
	t.Helper()

	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}

	bounds := img.Bounds()
	pixels := make([][3]uint8, 0, bounds.Dx()*bounds.Dy())

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			// image/color returns 16-bit values; the high byte is the
			// original 8-bit channel.
			pixels = append(pixels, [3]uint8{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)})
		}
	}

	return pixels
}

func TestPPMAndPNGProduceIdenticalBytes(t *testing.T) {
	mappers := []*tonemap.ToneMapper{
		tonemap.NewToneMapper(0, tonemap.LinearCurve),
		tonemap.NewToneMapper(-1.5, tonemap.ReinhardCurve),
		tonemap.NewToneMapper(2, tonemap.ACESCurve),
	}

	for _, tm := range mappers {
		var ppm bytes.Buffer
		var pngBuf bytes.Buffer

		renderInto(t, &ppm, PPMFormat, tm)
		renderInto(t, &pngBuf, PNGFormat, tm)

		ppmPixels := decodePPM(t, ppm.Bytes())
		pngPixels := decodePNG(t, pngBuf.Bytes())

		if len(ppmPixels) != len(pngPixels) {
			t.Fatalf("pixel count differs: ppm=%d png=%d", len(ppmPixels), len(pngPixels))
		}

		for i := range ppmPixels {
			if ppmPixels[i] != pngPixels[i] {
				t.Errorf("mapper %s/%g: pixel %d differs ppm=%v png=%v",
					tm.Curve, tm.Exposure, i, ppmPixels[i], pngPixels[i])
			}
		}
	}
}

func TestPPMHeaderFormat(t *testing.T) {
	var buf bytes.Buffer
	renderInto(t, &buf, PPMFormat, tonemap.DefaultToneMapper())

	header := strings.SplitN(buf.String(), "\n", 4)
	if header[0] != "P3" || header[1] != "7 1" || header[2] != "255" {
		t.Fatalf("unexpected PPM header: %q", strings.Join(header[:3], "\n"))
	}
}

func TestPNGIsValidDecodableFile(t *testing.T) {
	var buf bytes.Buffer
	renderInto(t, &buf, PNGFormat, tonemap.DefaultToneMapper())

	img, err := png.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("produced bytes are not a valid PNG: %v", err)
	}

	if img.Bounds().Dx() != 7 || img.Bounds().Dy() != 1 {
		t.Fatalf("PNG bounds = %v, want 7x1", img.Bounds())
	}
}

func TestSpecialPixelsHaveDefinedOutput(t *testing.T) {
	var buf bytes.Buffer
	renderInto(t, &buf, PNGFormat, tonemap.NewToneMapper(0, tonemap.ACESCurve))
	pixels := decodePNG(t, buf.Bytes())

	black := pixels[0]
	if black != [3]uint8{0, 0, 0} {
		t.Errorf("all-black pixel encoded as %v", black)
	}

	special := pixels[len(pixels)-1] // NaN, +Inf, negative
	if special != [3]uint8{0, 255, 0} {
		t.Errorf("NaN/+Inf/negative pixel = %v, want {0 255 0}", special)
	}

	// Extremely bright finite radiance saturates but must never produce a
	// value outside the byte range.
	bright := pixels[len(pixels)-2]
	for _, channel := range bright {
		if channel == 0 {
			t.Errorf("bright pixel contains a zero channel: %v", bright)
		}
	}
}

func TestLegacyLinearCurveMatchesClampBehaviour(t *testing.T) {
	// With the linear curve the tone mapper is the identity in range and
	// hard clips above 1, reproducing the pre-tonemapping output.
	tm := tonemap.NewToneMapper(0, tonemap.LinearCurve)

	cases := []struct {
		in   float64
		want uint8
	}{
		{0, 0},
		{0.25, uint8(256 * clampGamma(math.Sqrt(0.25)))},
		{1, 255},
		{8, 255},
	}

	for _, tc := range cases {
		mapped := tm.MapChannel(tc.in)
		got := uint8(256 * clampGamma(math.Sqrt(mapped)))

		if got != tc.want {
			t.Errorf("linear legacy mapping of %g = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func clampGamma(x float64) float64 {
	if x < 0 {
		return 0
	}

	if x > 0.999 {
		return 0.999
	}

	return x
}

func TestOversizedAndInvalidDimensions(t *testing.T) {
	badDimensions := []struct {
		width, height int
	}{
		{0, 10},
		{10, 0},
		{-1, 10},
		{1 << 20, 1 << 20}, // 1.1e12 pixels, far above MaxImagePixels
	}

	for _, dims := range badDimensions {
		var buf bytes.Buffer
		w := NewWriter("", WithOutput(&buf), WithFormat(PPMFormat))
		err := w.WriteHeader(dims.width, dims.height)

		if !errors.Is(err, ErrInvalidDimensions) {
			t.Errorf("WriteHeader(%d,%d) err = %v, want ErrInvalidDimensions", dims.width, dims.height, err)
		}

		// Close keeps surfacing the first recorded failure.
		if closeErr := w.Close(); !errors.Is(closeErr, ErrInvalidDimensions) {
			t.Errorf("Close after failed header err = %v, want ErrInvalidDimensions", closeErr)
		}

		if buf.Len() != 0 {
			t.Errorf("failed header wrote %d bytes, expected no output", buf.Len())
		}
	}
}

func TestHeaderOrderingAndPixelCount(t *testing.T) {
	t.Run("pixel before header", func(t *testing.T) {
		var buf bytes.Buffer
		w := NewWriter("", WithOutput(&buf))

		if err := w.WriteColor(color.White()); !errors.Is(err, ErrHeaderNotWritten) {
			t.Fatalf("err = %v, want ErrHeaderNotWritten", err)
		}
	})

	t.Run("double header", func(t *testing.T) {
		var buf bytes.Buffer
		w := NewWriter("", WithOutput(&buf))

		if err := w.WriteHeader(1, 1); err != nil {
			t.Fatalf("first header: %v", err)
		}

		if err := w.WriteHeader(1, 1); !errors.Is(err, ErrHeaderNotWritten) {
			t.Fatalf("second header err = %v, want ErrHeaderNotWritten", err)
		}
	})

	t.Run("too many pixels", func(t *testing.T) {
		var buf bytes.Buffer
		w := NewWriter("", WithOutput(&buf), WithFormat(PNGFormat))

		if err := w.WriteHeader(1, 1); err != nil {
			t.Fatalf("header: %v", err)
		}

		if err := w.WriteColor(color.Black()); err != nil {
			t.Fatalf("first pixel: %v", err)
		}

		if err := w.WriteColor(color.White()); !errors.Is(err, ErrTooManyPixels) {
			t.Fatalf("second pixel err = %v, want ErrTooManyPixels", err)
		}
	})

	t.Run("incomplete image", func(t *testing.T) {
		var buf bytes.Buffer
		w := NewWriter("", WithOutput(&buf), WithFormat(PNGFormat))

		if err := w.WriteHeader(2, 2); err != nil {
			t.Fatalf("header: %v", err)
		}

		if err := w.WriteColor(color.Black()); err != nil {
			t.Fatalf("pixel: %v", err)
		}

		if err := w.Close(); !errors.Is(err, ErrIncompleteImage) {
			t.Fatalf("Close err = %v, want ErrIncompleteImage", err)
		}

		// A failed PNG must never leave a decodable image behind.
		if _, err := png.Decode(bytes.NewReader(buf.Bytes())); err == nil {
			t.Fatal("incomplete write produced a decodable PNG")
		}
	})

	t.Run("close without header", func(t *testing.T) {
		var buf bytes.Buffer
		w := NewWriter("", WithOutput(&buf))

		if err := w.Close(); !errors.Is(err, ErrHeaderNotWritten) {
			t.Fatalf("Close err = %v, want ErrHeaderNotWritten", err)
		}
	})
}

func TestInterruptedWriteSurfacesError(t *testing.T) {
	failing := &failingWriter{failAfter: 32}
	w := NewWriter("", WithOutput(failing), WithFormat(PPMFormat))

	if err := w.WriteHeader(4, 4); err != nil {
		t.Fatalf("header: %v", err)
	}

	var writeErr error

	for i := 0; i < 16; i++ {
		if err := w.WriteColor(color.NewColor(0.5, 0.5, 0.5)); err != nil {
			writeErr = err

			break
		}
	}

	if writeErr == nil {
		writeErr = w.Close()
	}

	if writeErr == nil || writeErr.Error() != "simulated write interruption" {
		t.Fatalf("expected interruption error, got %v", writeErr)
	}

	// Close stays idempotent and keeps reporting the first failure.
	if err := w.Close(); err == nil {
		t.Fatal("second Close swallowed the recorded write error")
	}

	// The partial bytes written before the interruption remain available
	// rather than being silently discarded.
	if failing.buf.Len() == 0 {
		t.Error("no partial output was retained after the interruption")
	}
}

func TestPNGEncodeErrorIsReturned(t *testing.T) {
	// A writer that accepts all pixel buffering but fails at encode time.
	failing := &failingWriter{failAfter: math.MaxInt}
	w := NewWriter("", WithOutput(failing), WithFormat(PNGFormat))

	if err := w.WriteHeader(1, 1); err != nil {
		t.Fatalf("header: %v", err)
	}

	if err := w.WriteColor(color.Black()); err != nil {
		t.Fatalf("pixel: %v", err)
	}

	// Force failure right before the PNG stream is emitted.
	failing.failAfter = 0

	if err := w.Close(); err == nil {
		t.Fatal("PNG encode failure was not surfaced on Close")
	}
}

func TestFileBackedRoundTrip(t *testing.T) {
	for _, format := range []Format{PPMFormat, PNGFormat} {
		tmp := filepath.Join(t.TempDir(), "image."+format.String())
		w := NewWriter(tmp, WithToneMapper(tonemap.NewToneMapper(-0.5, tonemap.ReinhardCurve)))

		if w.Format() != format {
			t.Fatalf("extension %s selected format %v", format, w.Format())
		}

		if err := w.WriteHeader(2, 1); err != nil {
			t.Fatalf("header: %v", err)
		}

		if err := w.WriteColor(color.NewColor(0.2, 0.4, 0.8)); err != nil {
			t.Fatalf("pixel 1: %v", err)
		}

		if err := w.WriteColor(color.NewColor(4, 2, 1)); err != nil {
			t.Fatalf("pixel 2: %v", err)
		}

		if err := w.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}

		info, err := os.Stat(tmp)
		if err != nil {
			t.Fatalf("output file missing: %v", err)
		}

		if info.Size() == 0 {
			t.Fatal("output file is empty")
		}
	}
}
