package mygo

import "time"

// What the system does not say of the user's regional formats: the first
// day of the week and the hour cycle of a region, from CLDR's week and
// time data, for a libc without them (musl) or a locale the system has
// not generated, and browsers that do not tell. Only the regions where
// they differ from Monday and 24 hours are listed.

// firstWeekday returns the first day of the week in a region, as "US".
func firstWeekday(region string) time.Weekday {
	switch region {
	case "MV":
		return time.Friday
	case "AE", "AF", "BH", "DJ", "DZ", "EG", "IQ", "IR", "JO", "KW", "LY", "OM", "QA", "SD", "SY":
		return time.Saturday
	case "AG", "AS", "BD", "BR", "BS", "BT", "BW", "BZ", "CA", "CO", "DM", "DO", "ET", "GT", "GU", "HK", "HN", "ID",
		"IL", "IN", "JM", "JP", "KE", "KH", "KR", "LA", "MH", "MM", "MO", "MT", "MX", "MZ", "NI", "NP", "PA", "PE",
		"PH", "PK", "PR", "PT", "PY", "SA", "SG", "SV", "TH", "TT", "TW", "UM", "US", "VE", "VI", "WS", "YE", "ZA", "ZW":
		return time.Sunday
	}
	return time.Monday
}

// hourCycle returns the hour cycle a region prefers, 12 or 24.
func hourCycle(region string) int {
	switch region {
	case "AS", "AU", "BD", "CA", "CO", "EG", "GU", "IN", "JO", "KR", "MP", "MX", "MY", "NZ", "PH", "PK", "PR", "SA",
		"SG", "TW", "UM", "US", "VI":
		return 12
	}
	return 24
}
