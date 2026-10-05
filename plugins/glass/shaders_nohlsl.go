//go:build !windows

package glass

// The Direct3D bytecode compiled ahead of time is for Windows alone.
var hlslBytecode []byte

const hlslSum = ""
