//go:build !darwin

package glass

// The Metal library compiled ahead of time is for macOS alone.
var metalLibrary []byte

const metalSum = ""
