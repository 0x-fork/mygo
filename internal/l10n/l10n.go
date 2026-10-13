// Package l10n holds MyGo's own strings, those of the controls of package
// ui, of the code editor and the diff viewer, and of the roles of menus,
// in the languages MyGo is translated to: a library's catalog (see
// i18n.NewLibraryCatalog) of the namespace mygo, whose functions say them
// in the language the app shows. An app changes them, or translates them
// to its other languages, with keys "mygo.<key>" in its own catalogs, as
// "mygo.cancel" in its de.json.
//
// The catalogs are en.json, the source, zh-Hans.json and zh-Hant.json;
// catalog_gen.go is generated from them by `go generate`, by the code
// generator of apps' catalogs (internal/i18ngen).
package l10n

import "io/fs"

//go:generate go run gen.go

// Namespace is the namespace of the keys with which apps change MyGo's
// strings: "mygo.<key>".
const Namespace = "mygo"

// FS returns the catalogs, which `mygo generate` checks the keys
// "mygo.<key>" of apps against.
func FS() fs.FS { return files }
