// Package writer renders the camera's linear HDR output into an 8-bit
// image file.
//
// Two formats are supported:
//
//   - PPM (P3), the historical default. A .ppm/.pnm extension selects it,
//     and it is also the fallback when the extension is unknown so that
//     existing file names keep producing PPM output.
//   - PNG, selected by a .png extension or the explicit WithFormat option.
//
// Both formats share one encoding pipeline: the accumulated linear color
// is tone mapped (exposure + highlight compression), gamma encoded and
// finally quantized to 8 bits. Consequently the visible colors produced
// for identical linear input are identical between formats.
package writer

import (
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"ray-tracer/color"
	"ray-tracer/tonemap"
	"ray-tracer/util"
	"strings"
)

// Format selects the on-disk image container.
type Format int

const (
	// PPMFormat is the plain text PPM (P3) format and the historical
	// default of the renderer.
	PPMFormat Format = iota

	// PNGFormat is the 8-bit PNG format.
	PNGFormat
)

// String returns the lowercase extension-like name of the format.
func (f Format) String() string {
	switch f {
	case PNGFormat:
		return "png"
	default:
		return "ppm"
	}
}

// MaxImagePixels bounds the number of pixels a single image may contain.
// It rejects absurd dimensions before a huge raster allocation happens.
const MaxImagePixels = 1 << 28 // ~268 million pixels

var (
	// ErrInvalidDimensions is returned when the header dimensions are not
	// positive or exceed MaxImagePixels.
	ErrInvalidDimensions = errors.New("writer: image dimensions must be positive and within MaxImagePixels")

	// ErrHeaderNotWritten is returned when pixels are written before the
	// header, when a second header is written, or when the writer is
	// closed without ever writing a header.
	ErrHeaderNotWritten = errors.New("writer: WriteHeader must be called once before WriteColor")

	// ErrTooManyPixels is returned when more pixels are written than the
	// dimensions declared in the header.
	ErrTooManyPixels = errors.New("writer: more pixels written than declared by the header")

	// ErrIncompleteImage is returned on Close when fewer pixels were
	// written than the dimensions declared by the header.
	ErrIncompleteImage = errors.New("writer: closed before every declared pixel was written")
)

// byteRange is the display range before 8-bit quantization. The exclusive
// upper bound matches the historical int(256 * clamp(x)) behaviour, which
// can never emit 256.
var byteRange = util.NewInterval(0.0, 0.999)

// Option customizes a Writer at construction time.
type Option func(*Writer)

// WithFormat forces the output format instead of inferring it from the
// file name extension.
func WithFormat(format Format) Option {
	return func(w *Writer) {
		w.format = format
	}
}

// WithToneMapper sets the tone mapper applied to every linear HDR pixel.
func WithToneMapper(tm *tonemap.ToneMapper) Option {
	return func(w *Writer) {
		w.toneMapper = tm
	}
}

// WithOutput redirects the encoded bytes to the given writer instead of
// creating a file from Name. It is mainly useful for tests and streaming
// callers; the injected writer is never closed by the Writer, so callers
// retain ownership of its lifecycle.
func WithOutput(output io.Writer) Option {
	return func(w *Writer) {
		w.output = output
	}
}

// Writer serializes a rendered image. The zero value is not usable; create
// one with NewWriter.
type Writer struct {
	// Name is the destination file path. It may be empty when WithOutput
	// redirects the bytes elsewhere.
	Name   string
	output io.Writer
	closer io.Closer
	format Format

	toneMapper *tonemap.ToneMapper

	pixelCount int64
	written    int64
	headerDone bool

	// raster buffers PNG pixels until Close, where the file signature and
	// chunked body are emitted in one valid stream.
	raster *image.NRGBA

	// writeErr records the first I/O failure. After a failure every
	// subsequent write is a no-op and the error is surfaced on Close, so
	// an interrupted write always has a defined, observable result.
	writeErr error
	closed   bool
}

// NewWriter creates a Writer for the given file path. The format is
// inferred from the extension (.png -> PNG, everything else -> PPM) and
// can be overridden with WithFormat. It panics if the file cannot be
// created, preserving the historical constructor behaviour.
func NewWriter(name string, options ...Option) *Writer {
	w := &Writer{
		Name:       name,
		format:     formatFromName(name),
		toneMapper: tonemap.DefaultToneMapper(),
	}

	for _, option := range options {
		option(w)
	}

	if w.output == nil {
		file, err := os.Create(name)
		if err != nil {
			panic(err)
		}

		w.output = file
		w.closer = file
	}

	return w
}

// Format returns the selected output container.
func (w *Writer) Format() Format {
	return w.format
}

// WriteHeader declares the image dimensions and emits any format preamble.
// It returns ErrInvalidDimensions for non-positive or oversized
// dimensions and surfaces I/O errors from the underlying writer.
func (w *Writer) WriteHeader(width, height int) error {
	if w.writeErr != nil {
		return w.writeErr
	}

	if w.headerDone {
		return w.fail(ErrHeaderNotWritten)
	}

	if width <= 0 || height <= 0 {
		return w.fail(ErrInvalidDimensions)
	}

	pixels := int64(width) * int64(height)
	if pixels <= 0 || pixels > MaxImagePixels {
		return w.fail(fmt.Errorf("%w: got %dx%d", ErrInvalidDimensions, width, height))
	}

	w.pixelCount = pixels
	w.headerDone = true

	if w.format == PNGFormat {
		// image.NewNRGBA itself rejects dimensions whose byte size does
		// not fit in int; the check above already bounds the pixel count.
		w.raster = image.NewNRGBA(image.Rect(0, 0, width, height))

		return nil
	}

	if _, err := fmt.Fprintf(w.output, "P3\n%d %d\n255\n\n", width, height); err != nil {
		return w.fail(err)
	}

	return nil
}

// WriteColor tone maps, gamma encodes and quantizes one linear HDR pixel.
// PPM pixels are streamed immediately; PNG pixels are buffered until
// Close. NaN and Inf channels are handled by the tone mapper (black and
// white respectively).
func (w *Writer) WriteColor(c *color.Color) error {
	if w.writeErr != nil {
		return w.writeErr
	}

	if !w.headerDone {
		return w.fail(ErrHeaderNotWritten)
	}

	if w.written >= w.pixelCount {
		return w.fail(ErrTooManyPixels)
	}

	r, g, b := w.encode(c)
	index := w.written
	w.written++

	if w.format == PNGFormat {
		offset := int(index) * 4
		w.raster.Pix[offset+0] = r
		w.raster.Pix[offset+1] = g
		w.raster.Pix[offset+2] = b
		w.raster.Pix[offset+3] = 255

		return nil
	}

	if _, err := fmt.Fprintf(w.output, "%d %d %d\n", r, g, b); err != nil {
		return w.fail(err)
	}

	return nil
}

// Close flushes buffered output (the full PNG stream) and releases the
// underlying file. It is idempotent and returns the first I/O error
// encountered during the whole write, which is how an interrupted write
// is reported to the caller.
func (w *Writer) Close() error {
	if w.closed {
		return w.writeErr
	}

	w.closed = true

	if w.writeErr == nil {
		if !w.headerDone {
			w.fail(ErrHeaderNotWritten)
		} else if w.written < w.pixelCount {
			w.fail(ErrIncompleteImage)
		}
	}

	if w.writeErr == nil && w.format == PNGFormat {
		if err := png.Encode(w.output, w.raster); err != nil {
			w.fail(err)
		}
	}

	if w.closer != nil {
		if err := w.closer.Close(); err != nil && w.writeErr == nil {
			w.writeErr = err
		}
	}

	return w.writeErr
}

// encode runs the shared linear -> display -> 8-bit pipeline.
func (w *Writer) encode(c *color.Color) (uint8, uint8, uint8) {
	mapped := w.toneMapper.MapColor(c)

	r := util.LinearToGamma(mapped.R)
	g := util.LinearToGamma(mapped.G)
	b := util.LinearToGamma(mapped.B)

	return uint8(256 * byteRange.Clamp(r)),
		uint8(256 * byteRange.Clamp(g)),
		uint8(256 * byteRange.Clamp(b))
}

// fail records and returns the first error encountered by the writer.
func (w *Writer) fail(err error) error {
	if w.writeErr == nil {
		w.writeErr = err
	}

	return w.writeErr
}

// formatFromName maps a file name to an output format. Unknown and empty
// extensions default to PPM so that the legacy behaviour is preserved.
func formatFromName(name string) Format {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return PNGFormat
	default:
		return PPMFormat
	}
}
