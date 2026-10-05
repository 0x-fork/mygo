//go:build !darwin && !windows

package main

import "errors"

func compile() (name, compiled string, code []byte, err error) {
	return "", "", nil, errors.New("the glass's shaders compile on macOS and Windows")
}
