package i18n

import "time"

// Region is how a reader writes dates, times and numbers: the regional
// formats of their system, with the changes they made to them, which may
// be those of another language than the one they read, as an English
// reader in Germany who wants weeks to start on Monday and the time in 24
// hours. MyGo's App.Region reports it, and native UI reads it with
// Context.Region.
type Region struct {
	// Locale is the locale of the formats, a BCP 47 tag such as "en-GB"
	// or "de-DE", or "" when the system names none.
	Locale string
	// FirstWeekday is the day calendars start their weeks on.
	FirstWeekday time.Weekday
	// HourCycle is 12 for times with AM and PM, 24 for times without.
	HourCycle int
}
