//go:build !js

package mygo

import (
	"bytes"
	"image/png"
	"path/filepath"
	"testing"
)

func TestFileIcon(t *testing.T) {
	dir := t.TempDir()
	data, err := Shell.FileIcon(dir, 24)
	if err != nil {
		t.Fatal(err)
	}
	if img, err := png.Decode(bytes.NewReader(data)); err != nil || img.Bounds().Dx() != 24 {
		t.Errorf("FileIcon: %v, %v", img, err)
	}
	if _, err := Shell.FileIcon(dir, 0); err == nil {
		t.Error("FileIcon of size 0 did not fail")
	}
	if _, err := Shell.FileIcon(filepath.Join(dir, "missing"), 16); err == nil {
		t.Error("FileIcon of a missing file did not fail")
	}
}
