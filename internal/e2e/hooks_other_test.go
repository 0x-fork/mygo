//go:build !darwin && !(linux && (amd64 || arm64)) && !(windows && (amd64 || arm64))

package e2e

import (
	"errors"

	"github.com/egoist/mygo"
)

// Platforms without a backend run no GUI tests.
func activateMenu(*mygo.Window, ...string) error { return errors.New("unsupported") }

func pressShortcut(string, bool) (bool, bool) { return false, false }

func endSheet(*mygo.Window) (bool, bool) { return false, false }

func click(*mygo.Window, float64, float64) bool { return false }

func webViewAttached(*mygo.Window) (bool, bool) { return false, false }

func dismissPopups() (int, bool) { return 0, false }
