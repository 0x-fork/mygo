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
		{In("zh-Hant").MatchesMore(3, 1000), "第 3 項，共 1000 項以上"},
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
