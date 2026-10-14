package l10n

import (
	"bytes"
	"os"
	"testing"

	"github.com/egoist/mygo/internal/i18ngen"
)

// TestCatalogs checks the catalogs as `mygo generate` checks an app's: no
// errors, no warnings, every message in every language, and the accessors
// generated from them.
func TestCatalogs(t *testing.T) {
	c, diags, err := i18ngen.Load(FS(), i18ngen.Options{Dir: "internal/l10n", Source: "en", Namespace: Namespace})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range diags {
		t.Error(d)
	}
	if len(c.Languages) != 3 {
		t.Errorf("languages %v", c.Languages)
	}
	src, err := i18ngen.Go(c, "l10n")
	if err != nil {
		t.Fatal(err)
	}
	if old, _ := os.ReadFile(i18ngen.GoFile); !bytes.Equal(old, src) {
		t.Errorf("%s is out of date: go generate ./internal/l10n", i18ngen.GoFile)
	}
}

func TestMessages(t *testing.T) {
	for _, c := range []struct{ got, want string }{
		{Undo(), "Undo"}, // the source language before package mygo tells the app's
		{In("zh-Hans").Undo(), "撤销"},
		{In("zh-TW").Undo(), "復原"},
		{In("zh-HK").MenuOpenRecent(), "打開最近使用過的檔案"},
		{In("de").Undo(), "Undo"},
		{In("en").MenuQuitApp("Notes"), "Quit Notes"},
		{In("zh-Hans").MenuQuitApp("Notes"), "退出Notes"},
		{In("zh-Hant").MenuAbout("Notes"), "關於Notes"},
		{In("en").UnmodifiedLines(1), "1 unmodified line"},
		{In("en").UnmodifiedLines(4), "4 unmodified lines"},
		{In("zh-Hans").UnmodifiedLines(4), "4 行未修改"},
		{In("en").Matches(3, 12), "3 of 12"},
		{In("zh-Hant").MatchesMore(3, 1000), "第 3 項，共 1,000 項以上"},
		{In("en").Line(12345), "Line 12345"},
		{In("en").UntitledNumbered(2), "Untitled 2"},
		{In("zh-Hans").DocumentFailed("moved", "a.txt"), "无法移动文稿“a.txt”。"},
		{In("en").DocumentFailed("saved", "a.txt"), "The document “a.txt” could not be saved."},
		{In("en").ColorValue(0.4, 0.755), "40% saturation, 76% brightness"},
		{In("en").CurrentLine(3, 120, 5), "Current line 3 of 120, column 5"},
		{In("zh-Hans").LineRange(120), "第 1 到 120 行，可加 :列"},
		{In("en").RemoveToken("Ada"), "Remove Ada"},
		{In("en").HunkAction(In("en").Revert()), "Revert change"},
		{In("en-XA").Close(), "[Çļöšé one]"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

// TestTextsAllocateNothing checks what the controls rely on as they say
// their strings every frame: a string without arguments, said again in
// the same language, allocates nothing.
func TestTextsAllocateNothing(t *testing.T) {
	for _, locale := range []string{"", "en", "zh-Hans", "zh-TW", "en-XA"} {
		want := In(locale).Undo()
		if allocs := testing.AllocsPerRun(100, func() {
			if In(locale).Undo() != want {
				t.Fatal("the string changed")
			}
		}); allocs != 0 {
			t.Errorf("Undo in %q allocates %v times", locale, allocs)
		}
	}
}

func BenchmarkText(b *testing.B) {
	In("zh-Hans").Undo()
	b.ReportAllocs()
	for b.Loop() {
		_ = In("zh-Hans").Undo()
	}
}
