// Package random holds the renderer-wide random source.
//
// All rendering randomness (camera samples, material scattering, Perlin
// permutations, ...) goes through the package-level generator so that a
// whole render can be made deterministic by calling Seed. Two renders
// seeded identically consume the same random stream, which makes the
// output of any output format (PPM/PNG) byte-for-byte comparable.
package random

import "math/rand/v2"

// DefaultPCGSeeds are the fixed state the generator starts from. They are
// arbitrary but constant, so a fresh program run is reproducible.
const (
	DefaultPCGSeed1 uint64 = 0x853c49e6748fea9b
	DefaultPCGSeed2 uint64 = 0xda3e39cb94b95bdb
)

// rng is the process-wide source. It is a PCG generator rather than the
// global math/rand/v2 functions because the global source cannot be
// re-seeded in rand/v2.
var rng = rand.NewPCG(DefaultPCGSeed1, DefaultPCGSeed2)

var source = rand.New(rng)

// Seed re-initializes the package-wide generator from the given state.
// Call it once before rendering to get a deterministic random stream.
func Seed(seed1, seed2 uint64) {
	rng.Seed(seed1, seed2)
}

// Float64 returns a pseudo-random number in the half-open interval
// [min, max). It panics if max <= min, mirroring the contract of a scaled
// rand.Float64.
func Float64(min float64, max float64) float64 {
	return min + (max-min)*source.Float64()
}

// UnitFloat64 returns a pseudo-random number in [0, 1).
func UnitFloat64() float64 {
	return source.Float64()
}

// Int32N returns a pseudo-random int32 in [0, bound). It panics if
// bound <= 0.
func Int32N(bound int32) int32 {
	return source.Int32N(bound)
}
