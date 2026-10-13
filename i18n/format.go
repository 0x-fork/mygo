package i18n

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Format writes the message with args, the values of its arguments by
// name, for locale: the language whose plural rules pick the cases of
// plural and selectordinal arguments, and whose conventions numbers and
// dates follow. An argument args lacks is written as it is in the message,
// {name}. The pseudo locales (IsPseudo) transform the message, which keeps
// the plural rules of English.
//
// Values: a string, an integer or floating-point number, a string of a
// decimal number (which keeps its fraction digits: "1.50"), a bool, a
// time.Time, an error, a fmt.Stringer, or anything fmt.Sprint writes.
func (m *Message) Format(locale string, args map[string]any) string {
	if IsPseudo(locale) {
		return m.format(locale, "en", args)
	}
	return m.format(locale, locale, args)
}

// format formats the message with the plural rules of pluralLocale, the
// language it is in, and numbers and dates for locale. A pseudo locale
// transforms it.
func (m *Message) format(locale, pluralLocale string, args map[string]any) string {
	f := formatter{locale: locale, pluralLocale: pluralLocale, args: args, pseudo: pseudoFor(locale)}
	if f.pseudo != nil {
		f.locale = pluralLocale
	}
	var b strings.Builder
	f.nodes(&b, m.nodes, nil)
	if f.pseudo != nil {
		return f.pseudo.wrap(b.String())
	}
	return b.String()
}

// formatter formats a message.
type formatter struct {
	locale       string // of numbers and dates
	pluralLocale string // whose plural rules apply: the message's language
	args         map[string]any
	pseudo       pseudoMethod
}

// pluralValue is the number of the plural argument whose case is being
// written, for #.
type pluralValue struct {
	num number
	ok  bool
	raw any
}

func (f *formatter) nodes(b *strings.Builder, nodes []node, pound *pluralValue) {
	for i := range nodes {
		n := &nodes[i]
		switch n.kind {
		case nodeText:
			if f.pseudo != nil {
				b.WriteString(f.pseudo.text(n.text))
			} else {
				b.WriteString(n.text)
			}
		case nodePound:
			if pound == nil {
				b.WriteByte('#')
				continue
			}
			var s string
			if pound.ok {
				s = formatNumber(f.locale, pound.num.decimal, "")
			} else {
				s = valueString(f.locale, pound.raw)
			}
			f.placeholder(b, s)
		default:
			f.argument(b, n, pound)
		}
	}
}

// placeholder writes what an argument wrote.
func (f *formatter) placeholder(b *strings.Builder, s string) {
	if f.pseudo != nil {
		s = f.pseudo.placeholder(s)
	}
	b.WriteString(s)
}

func (f *formatter) argument(b *strings.Builder, n *node, pound *pluralValue) {
	v, ok := f.args[n.text]
	if !ok {
		b.WriteString("{" + n.text + "}")
		return
	}
	switch n.kind {
	case nodeArg:
		f.placeholder(b, valueString(f.locale, v))
	case nodeNumber:
		if num, err := toNumber(v); err == nil {
			f.placeholder(b, formatNumber(f.locale, num.decimal, n.style))
		} else {
			f.placeholder(b, valueString(f.locale, v))
		}
	case nodeDate, nodeTime:
		if t, ok := v.(time.Time); ok {
			kind := "date"
			if n.kind == nodeTime {
				kind = "time"
			}
			f.placeholder(b, formatDate(f.locale, t, kind, n.style))
		} else {
			f.placeholder(b, valueString(f.locale, v))
		}
	case nodeSelect:
		key := selectKey(v)
		c := findCase(n.cases, key)
		f.nodes(b, c.nodes, pound)
	case nodePlural, nodeSelectOrdinal:
		num, err := toNumber(v)
		if err != nil {
			// Not a number: the other case, whose # writes the value.
			f.nodes(b, findCase(n.cases, "other").nodes, &pluralValue{raw: v})
			return
		}
		// An exact case matches the number itself, a keyword the number
		// less the offset.
		var chosen *msgCase
		if exact, err := parseOperands(num.decimal); err == nil && !strings.HasPrefix(num.decimal, "-") {
			for i := range n.cases {
				c := &n.cases[i]
				if strings.HasPrefix(c.key, "=") && c.exact.I == exact.I && c.exact.T == exact.T && c.exact.W == exact.W {
					chosen = c
					break
				}
			}
		}
		shown := num.minus(n.offset)
		if chosen == nil {
			o, _ := parseOperands(shown.decimal)
			var cat Plural
			if n.kind == nodePlural {
				cat = PluralCategory(f.pluralLocale, o)
			} else {
				cat = OrdinalCategory(f.pluralLocale, o)
			}
			chosen = findCase(n.cases, cat.String())
		}
		f.nodes(b, chosen.nodes, &pluralValue{num: shown, ok: true})
	}
}

// findCase returns the case of key, else other, which every argument has.
func findCase(cases []msgCase, key string) *msgCase {
	var other *msgCase
	for i := range cases {
		switch cases[i].key {
		case key:
			return &cases[i]
		case "other":
			other = &cases[i]
		}
	}
	return other
}

// selectKey returns the case a value of a select argument picks.
func selectKey(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case fmt.Stringer:
		return v.String()
	}
	if num, err := toNumber(v); err == nil {
		return num.decimal
	}
	return fmt.Sprint(v)
}

// number is a decimal number as it is written, with its sign: "-1.50".
type number struct {
	decimal string
}

var errNotNumber = errors.New("i18n: not a number")

// toNumber returns v as a decimal number when it is one: an integer, a
// finite float, a string of a decimal number, or Operands.
func toNumber(v any) (number, error) {
	switch v := v.(type) {
	case int:
		return number{strconv.FormatInt(int64(v), 10)}, nil
	case int8:
		return number{strconv.FormatInt(int64(v), 10)}, nil
	case int16:
		return number{strconv.FormatInt(int64(v), 10)}, nil
	case int32:
		return number{strconv.FormatInt(int64(v), 10)}, nil
	case int64:
		return number{strconv.FormatInt(v, 10)}, nil
	case uint:
		return number{strconv.FormatUint(uint64(v), 10)}, nil
	case uint8:
		return number{strconv.FormatUint(uint64(v), 10)}, nil
	case uint16:
		return number{strconv.FormatUint(uint64(v), 10)}, nil
	case uint32:
		return number{strconv.FormatUint(uint64(v), 10)}, nil
	case uint64:
		return number{strconv.FormatUint(v, 10)}, nil
	case uintptr:
		return number{strconv.FormatUint(uint64(v), 10)}, nil
	case float32:
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return number{}, errNotNumber
		}
		return number{strconv.FormatFloat(float64(v), 'f', -1, 32)}, nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return number{}, errNotNumber
		}
		return number{strconv.FormatFloat(v, 'f', -1, 64)}, nil
	case string:
		s := strings.TrimSpace(v)
		if !isDecimal(s) {
			return number{}, errNotNumber
		}
		return number{s}, nil
	case Operands:
		s := strconv.FormatUint(v.I, 10)
		if v.I >= big {
			s = strconv.FormatUint(v.I-big, 10)
		}
		if v.V > 0 {
			f := strconv.FormatUint(v.F%big, 10)
			s += "." + strings.Repeat("0", max(v.V-len(f), 0)) + f
		}
		return number{s}, nil
	}
	return number{}, errNotNumber
}

// isDecimal reports whether s is a decimal number: an optional minus
// sign, digits, and an optional fraction.
func isDecimal(s string) bool {
	s = strings.TrimPrefix(s, "-")
	i, f, point := strings.Cut(s, ".")
	return i != "" && digits(i) && digits(f) && (!point || f != "")
}

// minus returns n less offset, with as many fraction digits as n has.
func (n number) minus(offset int64) number {
	if offset == 0 {
		return n
	}
	if !strings.Contains(n.decimal, ".") {
		if i, err := strconv.ParseInt(n.decimal, 10, 64); err == nil && i >= math.MinInt64+offset {
			return number{strconv.FormatInt(i-offset, 10)}
		}
	}
	f, err := strconv.ParseFloat(n.decimal, 64)
	if err != nil {
		return n
	}
	_, frac, _ := strings.Cut(n.decimal, ".")
	return number{strconv.FormatFloat(f-float64(offset), 'f', len(frac), 64)}
}

// valueString writes the value of a simple argument.
func valueString(locale string, v any) string {
	switch v := v.(type) {
	case string:
		return v
	case time.Time:
		return formatDate(locale, v, "datetime", "short")
	case error:
		return v.Error()
	case fmt.Stringer:
		return v.String()
	case bool:
		return strconv.FormatBool(v)
	case nil:
		return ""
	}
	if num, err := toNumber(v); err == nil {
		return formatNumber(locale, num.decimal, "")
	}
	return fmt.Sprint(v)
}

// formatNumber writes a decimal number for locale in an ICU number style:
// "" (the default), "integer", "percent", or a skeleton ("::...").
// CLDR's number formats will replace the plain digits it writes until
// then.
var formatNumber = func(locale, decimal, style string) string {
	switch style {
	case "integer":
		f, err := strconv.ParseFloat(decimal, 64)
		if err != nil {
			return decimal
		}
		return strconv.FormatFloat(math.RoundToEven(f), 'f', 0, 64)
	case "percent":
		f, err := strconv.ParseFloat(decimal, 64)
		if err != nil {
			return decimal
		}
		return strconv.FormatFloat(math.RoundToEven(f*100), 'f', 0, 64) + "%"
	}
	return decimal
}

// formatDate writes a time for locale: its date, its time, or both
// ("datetime"), in an ICU style, "short", "medium", "long" or "full" (""
// is "medium"), or a skeleton ("::..."). CLDR's date formats will replace
// the ISO 8601 forms it writes until then.
var formatDate = func(locale string, t time.Time, kind, style string) string {
	switch kind {
	case "time":
		if style == "short" {
			return t.Format("15:04")
		}
		return t.Format("15:04:05")
	case "datetime":
		return t.Format("2006-01-02 15:04")
	}
	return t.Format("2006-01-02")
}
