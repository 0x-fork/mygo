package ui

import "github.com/egoist/mygo/i18n"

// Locale returns the language the app shows (mygo.App.Locale), a BCP 47
// tag such as "en" or "zh-Hans", for what the view says in it. Text is in
// it unless an element says otherwise (Element.Lang). A frame follows its
// changes, as App.SetLocale makes them.
func (c *context) Locale() string { return c.rt.lang }

// Region returns the user's regional formats (mygo.App.Region): the
// locale of the formats, the first day of the week and the hour cycle,
// which dates and times follow. A frame follows their changes.
func (c *context) Region() i18n.Region { return c.rt.host.region() }
