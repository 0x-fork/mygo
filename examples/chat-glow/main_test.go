package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// A suggestion asks the assistant, which thinks, the window glowing, then
// writes its reply word by word, and the glow fades once it is done.
func TestReply(t *testing.T) {
	a := newApp()
	tt := ui.NewTester(a.view, 860, 700)
	if err := tt.Click("Summarize my week"); err != nil {
		t.Fatal(err)
	}
	if len(a.messages) != 2 || !a.busy {
		t.Fatalf("%d messages, busy %v after a suggestion, want 2, busy", len(a.messages), a.busy)
	}
	tt.Frame()
	if !tt.HasText("Summarize my week") {
		t.Error("the question does not show")
	}
	reply := a.messages[1]
	a.step(thinkTime / 2)
	if reply.shown != 0 || a.glow <= 0 {
		t.Fatalf("%d words shown, glow %.2f while thinking, want none, glowing", reply.shown, a.glow)
	}
	a.step(thinkTime / 2)
	a.step(0.1)
	if reply.shown == 0 || reply.shown == len(reply.words) {
		t.Fatalf("%d of %d words shown a moment into writing, want some", reply.shown, len(reply.words))
	}
	for range 200 {
		a.step(0.05)
	}
	if a.busy || reply.shown != len(reply.words) || a.glow != 0 {
		t.Fatalf("busy %v, %d of %d words, glow %.2f long after, want done, all, no glow", a.busy, reply.shown, len(reply.words), a.glow)
	}
	if got := strings.Join(reply.words, ""); got != reply.text {
		t.Errorf("the words join into %q, want the reply", got)
	}
	tt.Frame()
	if !tt.HasText(reply.text) {
		t.Error("the reply does not show")
	}
}

// Return sends what was typed; Escape stops the assistant while it
// thinks.
func TestTypeAndStop(t *testing.T) {
	a := newApp()
	tt := ui.NewTester(a.view, 860, 700)
	tt.Type("Hello there")
	tt.Key(0, ui.KeyEnter)
	if len(a.messages) != 2 || a.messages[0].text != "Hello there" || a.input != "" {
		t.Fatalf("messages %d, input %q after Return, want the question sent", len(a.messages), a.input)
	}
	tt.Key(0, ui.KeyEscape)
	if a.busy || a.messages[1].shown != 1 || a.messages[1].words[0] != "Stopped." {
		t.Fatalf("busy %v, reply %v after Escape, want stopped", a.busy, a.messages[1].words)
	}
}

// splitWords keeps the spaces and line breaks, which joined give the text
// back.
func TestSplitWords(t *testing.T) {
	for _, s := range []string{"one", "one two", "a  b\n\nc ", "Day 1: x.\n\nDay 2"} {
		w := splitWords(s)
		if strings.Join(w, "") != s {
			t.Errorf("splitWords(%q) = %q", s, w)
		}
	}
	if n := len(splitWords("a b  c\n\nd")); n != 4 {
		t.Errorf("%d words in \"a b  c\\n\\nd\", want 4", n)
	}
}

// The conversation scrolls: once it is longer than the window, it shows
// its end, the whole of the last reply above the composer, not under it.
func TestScrollsToEnd(t *testing.T) {
	a := newApp()
	tt := ui.NewTester(a.view, 860, 500)
	for i := range 3 {
		if i == 0 {
			if err := tt.Click("Summarize my week"); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := tt.Click("Message"); err != nil {
				t.Fatal(err)
			}
			tt.Type("And then?")
			tt.Key(0, ui.KeyEnter)
		}
		// The reply comes in word by word, as it shows.
		for a.busy {
			a.step(0.1)
			tt.Frame()
		}
	}
	if len(a.messages) != 6 {
		t.Fatalf("%d messages, want 6", len(a.messages))
	}
	tt.Frame()
	if !a.list.AtEnd() {
		t.Error("the conversation does not show its end")
	}
	last := a.messages[len(a.messages)-1]
	r, ok := tt.Find(last.text)
	if !ok {
		t.Fatal("the last reply does not show")
	}
	field, _ := tt.Find("Message")
	if r.Y+r.H > field.Y-ringPadding {
		t.Errorf("the last reply ends at %.0f, under the composer at %.0f", r.Y+r.H, field.Y)
	}
	if r.H < 3*14 {
		t.Errorf("the last reply shows %.0f DIPs of its lines, want all of them", r.H)
	}
}
