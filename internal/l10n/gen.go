//go:build ignore

// Gen writes catalog_gen.go, the accessors of MyGo's own strings, with the
// generator of apps' catalogs: `go generate ./internal/l10n`.
package main

import (
	"bytes"
	"log"
	"os"

	"github.com/egoist/mygo/internal/i18ngen"
)

func main() {
	log.SetFlags(0)
	c, diags, err := i18ngen.Load(os.DirFS("."), i18ngen.Options{Dir: "internal/l10n", Source: "en", Namespace: "mygo"})
	if err != nil {
		log.Fatal(err)
	}
	for _, d := range diags {
		log.Print(d)
	}
	if diags.Errors() > 0 {
		os.Exit(1)
	}
	src, err := i18ngen.Go(c, "l10n")
	if err != nil {
		log.Fatal(err)
	}
	if old, _ := os.ReadFile(i18ngen.GoFile); !bytes.Equal(old, src) {
		if err := os.WriteFile(i18ngen.GoFile, src, 0o644); err != nil {
			log.Fatal(err)
		}
	}
}
