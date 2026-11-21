package config

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"ray-tracer/tonemap"
	"ray-tracer/writer"
	"strings"
	"testing"
)

func TestParseDefaults(t *testing.T) {
	cfg := Parse([]string{"render.ppm"})

	if cfg.OutputPath != "render.ppm" {
		t.Errorf("output path = %q", cfg.OutputPath)
	}

	if cfg.Exposure != 0 {
		t.Errorf("default exposure = %g, want 0", cfg.Exposure)
	}

	if cfg.Curve != tonemap.ACESCurve {
		t.Errorf("default curve = %v, want aces", cfg.Curve)
	}

	if cfg.FormatSet {
		t.Error("format should default to extension inference")
	}

	if Current != cfg {
		t.Error("Parse did not install Current")
	}
}

func TestParseAllFlags(t *testing.T) {
	cfg := Parse([]string{
		"-exposure", "-2.5",
		"-curve", "reinhard",
		"-format", "png",
		"-seed", "999",
		"out/image.png",
	})

	if cfg.Exposure != -2.5 {
		t.Errorf("exposure = %g, want -2.5", cfg.Exposure)
	}

	if cfg.Curve != tonemap.ReinhardCurve {
		t.Errorf("curve = %v, want reinhard", cfg.Curve)
	}

	if !cfg.FormatSet || cfg.Format != writer.PNGFormat {
		t.Errorf("format = %v (set=%v), want explicit png", cfg.Format, cfg.FormatSet)
	}

	if cfg.Seed != 999 {
		t.Errorf("seed = %d, want 999", cfg.Seed)
	}
}

func TestParseLinearCurveAndPPMFormat(t *testing.T) {
	cfg := Parse([]string{"-curve", "linear", "-format", "ppm", "out.ppm"})

	if cfg.Curve != tonemap.LinearCurve {
		t.Errorf("curve = %v, want linear", cfg.Curve)
	}

	if cfg.Format != writer.PPMFormat {
		t.Errorf("format = %v, want ppm", cfg.Format)
	}
}

func TestNewWriterUsesConfig(t *testing.T) {
	Parse([]string{"-exposure", "1", "-curve", "reinhard", "-format", "png", filepath.Join(t.TempDir(), "x.ppm")})

	// The explicit -format png must win over the .ppm extension.
	w := NewWriter()
	if w.Format() != writer.PNGFormat {
		t.Errorf("writer format = %v, want png from explicit flag", w.Format())
	}
}

func TestParseErrorsExit(t *testing.T) {
	cases := [][]string{
		{}, // missing output path
		{"-curve", "bogus", "out.ppm"},
		{"-format", "tiff", "out.ppm"},
		{"-exposure", "not-a-number", "out.ppm"},
	}

	for index, args := range cases {
		t.Run(strings.Join(args, "/"), func(t *testing.T) {
			if os.Getenv("CONFIG_PARSE_CHILD") == "1" {
				if os.Getenv("CONFIG_PARSE_CASE") != strings.TrimSpace(string(rune('0'+index))) {
					return
				}

				Parse(args)

				return
			}

			cmd := exec.Command(os.Args[0], "-test.run=^TestParseErrorsExit$")
			cmd.Env = append(os.Environ(),
				"CONFIG_PARSE_CHILD=1",
				"CONFIG_PARSE_CASE="+string(rune('0'+index)),
			)

			var stderr bytes.Buffer
			cmd.Stderr = &stderr

			err := cmd.Run()

			if err == nil {
				t.Fatal("expected non-zero exit for bad arguments")
			}

			exitErr, ok := err.(*exec.ExitError)
			if !ok || exitErr.ExitCode() != 2 {
				t.Fatalf("expected exit code 2, got %v", err)
			}
		})
	}
}
