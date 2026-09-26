//go:build windows

package mygo

import (
	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/unsupported"
)

func newBackend() platform.Backend { return unsupported.New() }
