//go:build js

package web

import (
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

func TestKeyOf(t *testing.T) {
	for _, tc := range []struct {
		key, code string
		want      platform.Key
	}{
		{"a", "KeyA", platform.KeyA},
		{"A", "KeyA", platform.KeyA},
		// Letters follow the layout: the key typing a z on a German
		// keyboard is Z, where QWERTY has Y.
		{"z", "KeyY", platform.KeyZ},
		// Shifted and composed characters go by the key.
		{"!", "Digit1", platform.Key1},
		{"å", "KeyA", platform.KeyA},
		{"ß", "Minus", platform.KeyMinus},
		{" ", "Space", platform.KeySpace},
		{"Enter", "Enter", platform.KeyEnter},
		{"ArrowLeft", "ArrowLeft", platform.KeyLeft},
		{"F12", "F12", platform.KeyF12},
		{"BrowserBack", "BrowserBack", platform.KeyBack},
		{"Unidentified", "", platform.KeyUnknown},
	} {
		if got := keyOf(tc.key, tc.code); got != tc.want {
			t.Errorf("keyOf(%q, %q) = %v, want %v", tc.key, tc.code, got, tc.want)
		}
	}
}

func TestAccelText(t *testing.T) {
	for _, tc := range []struct {
		accel string
		mac   bool
		want  string
	}{
		{"CmdOrCtrl+C", true, "⌘C"},
		{"CmdOrCtrl+C", false, "Ctrl+C"},
		{"CmdOrCtrl+Shift+z", true, "⌘⇧Z"},
		{"Alt+F4", false, "Alt+F4"},
		{"", true, ""},
	} {
		if got := accelText(tc.accel, tc.mac); got != tc.want {
			t.Errorf("accelText(%q, %v) = %q, want %q", tc.accel, tc.mac, got, tc.want)
		}
	}
}

func TestButtonOf(t *testing.T) {
	// DOM: 0 primary, 1 middle, 2 secondary; MyGo: 0 primary, 1
	// secondary, 2 middle.
	for dom, want := range []int{0, 2, 1} {
		if got := buttonOf(dom); got != want {
			t.Errorf("buttonOf(%d) = %d, want %d", dom, got, want)
		}
	}
}

func TestUnpremul(t *testing.T) {
	for _, tc := range []struct{ c, a, want byte }{{128, 128, 255}, {64, 128, 128}, {0, 10, 0}, {200, 255, 200}} {
		if got := unpremul(tc.c, tc.a); got != tc.want {
			t.Errorf("unpremul(%d, %d) = %d, want %d", tc.c, tc.a, got, tc.want)
		}
	}
}
