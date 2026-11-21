// Package config holds the process-wide render configuration parsed from
// the command line.
//
// The first positional argument is always the output file path; the
// remaining arguments are optional flags:
//
//	-exposure EV   exposure adjustment in stops (default 0, may be negative)
//	-curve NAME    linear | reinhard | aces (default aces)
//	-format NAME   auto | ppm | png (default auto, inferred from the extension)
//	-seed UINT     deterministic random seed (default fixed constant)
//
// The parsed value is stored package-wide because scene constructors have
// fixed no-argument signatures.
package config

import (
	"flag"
	"fmt"
	"os"
	"ray-tracer/random"
	"ray-tracer/tonemap"
	"ray-tracer/writer"
)

// Current is the active configuration, installed by Parse.
var Current *Config

// Config describes one renderer invocation.
type Config struct {
	// OutputPath is the image file path given as the first positional arg.
	OutputPath string

	// Exposure is the exposure adjustment in EV stops.
	Exposure float64

	// Curve is the highlight compression curve.
	Curve tonemap.Curve

	// Format is the output container. When FormatSet is false the format
	// is inferred from the output file extension.
	Format    writer.Format
	FormatSet bool

	// Seed initializes the deterministic random source.
	Seed uint64
}

// Parse reads the command line arguments (without the program name),
// installs the resulting configuration as Current, seeds the random
// source and returns the configuration. It exits the process with a
// usage message on malformed arguments or a missing output path.
func Parse(args []string) *Config {
	flags := flag.NewFlagSet("ray-tracer", flag.ContinueOnError)

	cfg := &Config{}

	flags.Float64Var(&cfg.Exposure, "exposure", 0, "exposure adjustment in EV stops (may be negative)")
	curveName := flags.String("curve", "aces", "highlight compression curve: linear|reinhard|aces")
	formatName := flags.String("format", "auto", "output format: auto|ppm|png (auto uses the file extension)")
	flags.Uint64Var(&cfg.Seed, "seed", random.DefaultPCGSeed1, "deterministic random seed")

	if err := flags.Parse(args); err != nil {
		fail(flags, err)
	}

	if flags.NArg() < 1 {
		fail(flags, fmt.Errorf("missing output file path"))
	}

	cfg.OutputPath = flags.Arg(0)

	switch *curveName {
	case "linear":
		cfg.Curve = tonemap.LinearCurve
	case "reinhard":
		cfg.Curve = tonemap.ReinhardCurve
	case "aces":
		cfg.Curve = tonemap.ACESCurve
	default:
		fail(flags, fmt.Errorf("unknown curve %q", *curveName))
	}

	switch *formatName {
	case "auto":
		// Keep FormatSet false so the writer falls back to the extension.
	case "ppm":
		cfg.Format = writer.PPMFormat
		cfg.FormatSet = true
	case "png":
		cfg.Format = writer.PNGFormat
		cfg.FormatSet = true
	default:
		fail(flags, fmt.Errorf("unknown format %q", *formatName))
	}

	random.Seed(cfg.Seed, random.DefaultPCGSeed2)

	Current = cfg

	return cfg
}

// NewWriter builds the output writer described by the current
// configuration, including the configured tone mapper and format.
func NewWriter() *writer.Writer {
	options := []writer.Option{
		writer.WithToneMapper(tonemap.NewToneMapper(Current.Exposure, Current.Curve)),
	}

	if Current.FormatSet {
		options = append(options, writer.WithFormat(Current.Format))
	}

	return writer.NewWriter(Current.OutputPath, options...)
}

// fail prints the error and flag usage, then terminates with the standard
// argument-error exit code.
func fail(flags *flag.FlagSet, err error) {
	fmt.Fprintf(os.Stderr, "ray-tracer: %s\n", err)
	flags.Usage()
	os.Exit(2)
}
