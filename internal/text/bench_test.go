package text

import "testing"

// BenchmarkLayout lays out a wrapped paragraph without the cache, as each
// keystroke in an editor does.
func BenchmarkLayout(b *testing.B) {
	s := Shared()
	p := Params{Text: "The quick brown fox jumps over the lazy dog, and then some more words follow to wrap the paragraph over a few lines.", Style: Style{Size: 14}, Width: 300}
	s.Layout(p)
	b.ResetTimer()
	for range b.N {
		s.mu.Lock()
		s.layout(p)
		s.mu.Unlock()
	}
}
