// Gallery tours MyGo's own user interface toolkit: a window drawn on the
// GPU from Go, without a web page. It shows layout, the widgets, text
// editing, a list of ten thousand rows with context menus, styling (grids,
// borders, gradients, text decorations, motion), custom drawing, overlays,
// file drops and updates from other goroutines.
//
//	go run ./examples/gallery
package main

import (
	"fmt"
	"log"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type gallery struct {
	win  *mygo.Window
	page string

	count    int
	agree    bool
	notify   bool
	size     string
	plan     string
	volume   float64
	name     string
	email    string
	bio      string
	filter   string
	picked   int
	starred  map[int]bool
	tab      int
	split    float32
	copies   float64
	file     int
	tree     map[string]bool
	leaf     string
	birthday time.Time
	dialog   bool
	menu     bool
	files    []string
	now      time.Time
	samples  []float64
	period   int
	pinned   bool
	fruit    string
	eased    bool
}

var pages = []string{"Overview", "Controls", "Text", "List", "Styling", "Drawing", "Overlays"}

// icon parses the shapes of a 24×24 stroked icon, drawn in currentColor
// as icon sets draw them.
func icon(shapes string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` + shapes + `</svg>`))
}

// The icons of the pages, and two for buttons.
var (
	pageIcons = map[string]*ui.SVG{
		"Overview": icon(`<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>`),
		"Controls": icon(`<path d="M4 7h10M18 7h2M4 17h2M10 17h10"/><circle cx="16" cy="7" r="2"/><circle cx="8" cy="17" r="2"/>`),
		"Text":     icon(`<path d="M5 6V5h14v1M12 5v14M9 19h6"/>`),
		"List":     icon(`<path d="M9 6h11M9 12h11M9 18h11M4 6h.01M4 12h.01M4 18h.01"/>`),
		"Styling":  icon(`<path d="M12 21a9 9 0 1 1 9-9c0 2.5-2 3.5-3.5 3.5H16a2 2 0 0 0-1.5 3.3c.4.5.4 2.2-2.5 2.2z"/><circle cx="7.5" cy="11" r="1"/><circle cx="11" cy="7" r="1"/><circle cx="16" cy="8.5" r="1"/>`),
		"Drawing":  icon(`<path d="M15 5l4 4M4 20l1-4.5L16.5 4a2.1 2.1 0 0 1 3 3L8 18.5z"/>`),
		"Overlays": icon(`<path d="M12 3 3 8l9 5 9-5z"/><path d="m3 13 9 5 9-5"/>`),
	}
	starIcon    = icon(`<path d="M12 3l2.7 5.6 6.1.9-4.4 4.3 1 6.1-5.4-2.9-5.4 2.9 1-6.1L3.2 9.5l6.1-.9z"/>`)
	checkIcon   = icon(`<path d="M20 6 9 17l-5-5"/>`)
	chevronIcon = icon(`<path d="m6 9 6 6 6-6"/>`)
	loaderIcon  = icon(`<path d="M21 12a9 9 0 1 1-6.2-8.6"/>`)
	// A picture in its own colors: gradients, a clip path, and a dot in
	// currentColor.
	badge = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 120 120">
	<defs>
		<linearGradient id="bg" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#6366f1"/><stop offset="1" stop-color="#ec4899"/></linearGradient>
		<radialGradient id="glow" cx=".35" cy=".3" r=".6"><stop offset="0" stop-color="#fff" stop-opacity=".55"/><stop offset="1" stop-color="#fff" stop-opacity="0"/></radialGradient>
		<clipPath id="round"><rect width="120" height="120" rx="28"/></clipPath>
	</defs>
	<g clip-path="url(#round)"><rect width="120" height="120" fill="url(#bg)"/><circle cx="42" cy="36" r="60" fill="url(#glow)"/></g>
	<path d="M34 78 60 34l26 44z" fill="none" stroke="#fff" stroke-width="9" stroke-linejoin="round"/>
	<circle cx="60" cy="64" r="7" fill="currentColor"/>
</svg>`))
)

func (g *gallery) view(c *ui.Context) {
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		g.sidebar(c)
		ui.Scroll(c).Grow(1).Padding(28, 32).Gap(18).Children(func() {
			ui.Text(c, g.page).FontSize(26).Bold()
			switch g.page {
			case "Overview":
				g.overview(c)
			case "Controls":
				g.controls(c)
			case "Text":
				g.text(c)
			case "List":
				g.list(c)
			case "Styling":
				g.styling(c)
			case "Drawing":
				g.drawing(c)
			case "Overlays":
				g.overlays(c)
			}
		})
	})
	// Ctrl+1…7 (Cmd on macOS) switch pages.
	for i, p := range pages {
		if c.Shortcut(ui.Cmd, ui.Key1+ui.Key(i)) {
			g.page = p
		}
	}
}

func (g *gallery) sidebar(c *ui.Context) {
	t := c.Theme()
	side := ui.Column(c).Width(200).Padding(16, 10).Gap(2).Background(t.Surface).Shrink(0)
	side.Children(func() {
		ui.Text(c, "MyGo UI").FontSize(13).Bold().TextColor(t.TextMuted).Padding(4, 10, 10)
		for _, p := range pages {
			item := ui.Row(c).Key(p).Padding(7, 10).Gap(10).Radius(6).Focusable()
			if p == g.page {
				item.Background(t.Accent).TextColor(t.AccentText)
			} else if item.Hovered() {
				item.Background(t.SurfaceHover)
			}
			if item.Clicked() {
				g.page = p
			}
			// Icons take the text color: the accent's when selected.
			item.Children(func() {
				ui.Icon(c, pageIcons[p]).FontSize(16)
				ui.Text(c, p)
			})
		}
		ui.Spacer(c)
		ui.Text(c, g.now.Format("15:04:05")).FontSize(12).TextColor(t.TextMuted).Padding(0, 10)
	})
}

func card(c *ui.Context, title string, body func()) *ui.Element {
	t := c.Theme()
	return ui.Column(c).Padding(18).Gap(12).Radius(10).Background(t.Background).Border(1, t.Border).
		Shadow(0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.06)).Children(func() {
		if title != "" {
			ui.Text(c, title).FontSize(15).Bold()
		}
		body()
	})
}

func (g *gallery) overview(c *ui.Context) {
	t := c.Theme()
	ui.Text(c, "Everything here is laid out with flexbox and grids and drawn by MyGo itself: no HTML, no JavaScript, no cgo. "+
		"The view is a Go function of the app's state that runs again after every event.").TextColor(t.TextMuted)
	ui.Row(c).Gap(16).Wrap().AlignItems(ui.Start).Children(func() {
		card(c, "Counter", func() {
			ui.Text(c, fmt.Sprint(g.count)).FontSize(40).Bold()
			ui.Row(c).Gap(8).Children(func() {
				if ui.Button(c, "−").Width(44).Clicked() {
					g.count--
				}
				if ui.PrimaryButton(c, "Increment").Clicked() {
					g.count++
				}
			})
		})
		card(c, "Live data", func() {
			ui.Text(c, "A goroutine pushes a sample every second with Window.Update.").TextColor(t.TextMuted).MaxWidth(260)
			g.sparkline(c).Size(260, 80)
		})
		card(c, "Files", func() {
			zone := ui.Column(c).Size(260, 80).Padding(8, 12).Gap(2).Radius(6).Background(t.Surface).
				Border(1, t.Border).Justify(ui.Center).AlignItems(ui.Center)
			if files := zone.DroppedFiles(); files != nil {
				g.files = files
			}
			if zone.FileDragOver() {
				zone.Border(2, t.Accent)
			}
			zone.Children(func() {
				if len(g.files) == 0 {
					ui.Text(c, "Drop files here").TextColor(t.TextMuted)
				}
				for i, f := range g.files {
					if i == 3 {
						ui.Text(c, fmt.Sprintf("and %d more", len(g.files)-i)).FontSize(12).TextColor(t.TextMuted)
						break
					}
					ui.Text(c, filepath.Base(f)).FontSize(12).MaxLines(1)
				}
			})
		})
	})
}

func (g *gallery) sparkline(c *ui.Context) *ui.Element {
	t := c.Theme()
	return ui.Box(c).Radius(6).Background(t.Surface).Draw(func(p *ui.Painter, r ui.Rect) {
		if len(g.samples) < 2 {
			return
		}
		var path ui.Path
		for i, v := range g.samples {
			x := r.X + 6 + float32(i)/float32(len(g.samples)-1)*(r.W-12)
			y := r.Y + r.H - 6 - float32(v)*(r.H-12)
			if i == 0 {
				path.MoveTo(x, y)
			} else {
				path.LineTo(x, y)
			}
		}
		p.StrokePath(&path, 2, t.Accent)
	})
}

func (g *gallery) controls(c *ui.Context) {
	t := c.Theme()
	card(c, "Choices", func() {
		ui.Checkbox(c, &g.agree, "I agree to the terms")
		ui.Row(c).Gap(10).Children(func() {
			ui.Switch(c, &g.notify).Label("Notifications")
			ui.Text(c, map[bool]string{true: "Notifications on", false: "Notifications off"}[g.notify])
		})
		ui.Row(c).Gap(18).Children(func() {
			for _, p := range []string{"Free", "Pro", "Team"} {
				ui.Radio(c, &g.plan, p, p)
			}
		})
		ui.Row(c).Gap(10).Children(func() {
			ui.Text(c, "Size")
			ui.Select(c, &g.size, []string{"Small", "Medium", "Large", "Extra large"})
		})
	})
	card(c, "Ranges", func() {
		ui.Row(c).Gap(12).Children(func() {
			ui.Slider(c, &g.volume, 0, 100).Label("Volume").Grow(1)
			ui.Textf(c, "%3.0f%%", g.volume).Width(48).TextAlign(ui.End)
		})
		ui.Progress(c, g.volume/100)
		ui.Progress(c, -1)
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Copies")
			ui.NumberInput(c, &g.copies, 1, 99, 1).Label("Copies")
		})
	})
	card(c, "Buttons", func() {
		ui.Row(c).Gap(8).Wrap().Children(func() {
			if ui.PrimaryButton(c, "Save").Clicked() {
				c.Toast("Saved")
			}
			ui.Button(c, "Cancel")
			ui.Button(c, "Disabled").Disabled(true)
			ui.Link(c, "Open mygo.dev", "https://github.com/egoist/mygo")
		})
		ui.Text(c, "Tab moves the focus; Enter or Space presses the focused button.").TextColor(t.TextMuted)
	})
	card(c, "Tabs and panes", func() {
		ui.Tabs(c, &g.tab, "Files", "Search", "History")
		ui.Split(c, &g.split, func() {
			ui.Column(c).Fill().Padding(10).Gap(6).Background(t.Surface).Children(func() {
				for _, name := range [][]string{{"main.go", "go.mod", "README.md"}, {"Results"}, {"Yesterday", "Last week"}}[g.tab] {
					ui.Text(c, name).SingleLine()
				}
			})
		}, func() {
			ui.Column(c).Fill().Padding(10).Children(func() {
				ui.Text(c, "Drag the divider, or focus it and press the arrows.").TextColor(t.TextMuted)
			})
		}).Height(140).Border(1, t.Border).Radius(t.Radius).Clip()
	})
	card(c, "Built on bases", func() {
		ui.Text(c, "Bases are the widgets without their look: the pointer, the keys, the focus and accessibility, styled here anew.").TextColor(t.TextMuted)
		ui.Row(c).Gap(16).Wrap().Children(func() {
			// A segmented control on TabsBase.
			tabs := ui.TabsBase(c, &g.period, 3)
			tabs.List.Padding(3).Radius(999).Background(t.Surface).Children(func() {
				for i, name := range []string{"Day", "Week", "Month"} {
					seg := tabs.Tab(i).Padding(5, 14).Radius(999)
					if i == g.period {
						seg.Background(t.Background).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.15))
					}
					seg.Children(func() { ui.Text(c, name) })
				}
			})
			// A pill that toggles, on SwitchBase.
			pill := ui.SwitchBase(c, &g.pinned).Gap(6).Padding(5, 12).Radius(999).Border(1, t.Border)
			if g.pinned {
				pill.Background(t.Accent).TextColor(t.AccentText).Border(1, t.Accent)
			}
			pill.Children(func() {
				ui.Icon(c, starIcon)
				ui.Text(c, "Starred")
			})
			// A select with check marks, on SelectBase.
			sel := ui.SelectBase(c, &g.fruit)
			sel.Trigger.Gap(6).Padding(6, 10).Radius(8).Border(1, t.Border).Children(func() {
				ui.Text(c, g.fruit)
				ui.Icon(c, chevronIcon).TextColor(t.TextMuted)
			})
			sel.Popup(func(panel *ui.Element) {
				panel.Margin(4, 0, 0, 0).Padding(4).Radius(10).Background(t.Background).Border(1, t.Border)
				panel.Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.15))
				for _, fruit := range []string{"Apple", "Banana", "Cherry", "Durian"} {
					item := sel.Item(fruit).Gap(8).Padding(6, 10).Radius(6)
					if item.Highlighted() {
						item.Background(t.Accent).TextColor(t.AccentText)
					}
					item.Children(func() {
						check := ui.Icon(c, checkIcon)
						if fruit != g.fruit {
							check.Opacity(0)
						}
						ui.Text(c, fruit)
					})
				}
			})
		})
	})
}

func (g *gallery) text(c *ui.Context) {
	t := c.Theme()
	card(c, "Form", func() {
		label := func(s string) { ui.Text(c, s).FontSize(12).Bold().TextColor(t.TextMuted) }
		label("Name")
		ui.TextInput(c, &g.name).Placeholder("Ada Lovelace").Label("Name")
		label("Email")
		in := ui.TextInput(c, &g.email).Placeholder("ada@example.com").Label("Email")
		if in.Submitted() {
			g.dialog = true
		}
		label("About you")
		ui.TextArea(c, &g.bio).Placeholder("Multiple lines, with undo, selection and input methods.").Label("About you").Height(110)
		ui.Textf(c, "%d characters", len([]rune(g.bio))).FontSize(12).TextColor(t.TextMuted)
		label("Birthday")
		ui.DateInput(c, &g.birthday).Label("Birthday")
	})
	card(c, "Typography", func() {
		ui.Text(c, "Display 28").FontSize(28).Bold()
		ui.Text(c, "Italic, underlined and struck through").Italic().Underline().Strikethrough()
		ui.RichText(c,
			ui.Span{Text: "Rich text mixes "}, ui.Span{Text: "bold", Weight: 700}, ui.Span{Text: ", "},
			ui.Span{Text: "colored", Color: t.Accent}, ui.Span{Text: ", "}, ui.Span{Text: "large", Size: 20},
			ui.Span{Text: " and "}, ui.Span{Text: "underlined", Underline: true}, ui.Span{Text: " runs in one paragraph."},
		)
		ui.Text(c, "Monospace: func main() {}").Font("monospace")
		ui.Text(c, "SPACED CAPITALS").FontSize(12).Bold().LetterSpacing(2).TextColor(t.TextMuted)
		ui.Text(c, "Tabular digits: 1,111.11 / 8,888.88").FontFeatures("tnum")
		ui.Text(c, "Mixed scripts: English, Ελληνικά, Русский, 日本語, 한국어, العربية, עברית, हिन्दी 🎉").Selectable()
		ui.Text(c, strings.Repeat("Long text wraps to the width it gets. ", 6)).TextColor(t.TextMuted)
		ui.Text(c, strings.Repeat("A single line that ends with an ellipsis when it does not fit. ", 4)).SingleLine()
	})
}

func (g *gallery) list(c *ui.Context) {
	t := c.Theme()
	ui.TextInput(c, &g.filter).Placeholder("Filter 10,000 rows").Label("Filter")
	var rows []int
	for i := 0; i < 10000; i++ {
		if g.filter == "" || strings.Contains(fmt.Sprint(i), g.filter) {
			rows = append(rows, i)
		}
	}
	ui.Textf(c, "%d rows; only those in view are built. Right-click one for its menu.", len(rows)).TextColor(t.TextMuted)
	ui.List(c, len(rows), 32, func(i int) {
		n := rows[i]
		row := ui.Row(c).Fill().PaddingX(12).Gap(10).Radius(6)
		switch {
		case n == g.picked:
			row.Background(t.Accent).TextColor(t.AccentText)
		case row.Hovered():
			row.Background(t.SurfaceHover)
		}
		if row.Clicked() {
			g.picked = n
		}
		row.ContextMenu(func(m *ui.Menu) {
			if m.Item("Pick").Chosen() {
				g.picked = n
			}
			if m.Item("Starred").Checked(g.starred[n]).Chosen() {
				g.starred[n] = !g.starred[n]
			}
			m.Separator()
			if m.Item("Copy Square").Chosen() {
				mygo.Clipboard.WriteText(fmt.Sprint(n * n))
			}
		})
		row.Children(func() {
			label := fmt.Sprintf("Row %d", n)
			if g.starred[n] {
				label += "  ★"
			}
			ui.Text(c, label).Grow(1)
			ui.Textf(c, "%d²  =  %d", n, n*n).Font("monospace").FontSize(12)
		})
	}).Height(420).Border(1, t.Border).Radius(8).Padding(4)
	ui.Row(c).Gap(18).AlignItems(ui.Stretch).Height(260).Children(func() {
		card(c, "Tree", func() {
			item := func(path, label string, children func()) {
				var open *bool
				if children != nil {
					o := g.tree[path]
					open = &o
					defer func() { g.tree[path] = o }()
				}
				if ui.TreeItem(c, label, open, children).Selected(g.leaf == path).Clicked() {
					g.leaf = path
				}
			}
			ui.Tree(c, func() {
				item("ui", "ui", func() {
					item("ui/widgets.go", "widgets.go", nil)
					item("ui/text", "text", func() {
						item("ui/text/layout.go", "layout.go", nil)
					})
				})
				item("go.mod", "go.mod", nil)
			})
		}).Width(220)
		files := []string{"report.pdf", "photo.jpg", "notes.md", "budget.xlsx", "slides.key", "song.mp3"}
		card(c, "Table", func() {
			cols := []ui.TableColumn{{Title: "Name"}, {Title: "Size", Width: 90, Align: ui.End}}
			if ui.Table(c, cols, len(files), &g.file, func(row, col int) {
				if col == 0 {
					ui.Text(c, files[row]).SingleLine()
				} else {
					ui.Textf(c, "%d KB", (row+1)*173)
				}
			}).Grow(1).Submitted() {
				c.Toast("Opened " + files[g.file])
			}
		}).Grow(1)
	})
}

func (g *gallery) styling(c *ui.Context) {
	t := c.Theme()
	ui.Text(c, "Grids, borders of each side, gradients, stripes, text decorations, and motion along easings.").TextColor(t.TextMuted)
	ui.Grid(c).Columns(2).Gap(16).Children(func() {
		card(c, "Grid", func() {
			ui.Grid(c).ColumnTracks(ui.FitContent(), ui.Fr(1), ui.Fr(1)).Gap(6).Children(func() {
				cell := func(s string) *ui.Element {
					return ui.Box(c).Padding(8, 10).Radius(6).Background(t.Surface).Children(func() { ui.Text(c, s).FontSize(12) })
				}
				cell("ColumnSpan(-1)").ColumnSpan(-1).Background(t.Accent).TextColor(t.AccentText)
				cell("FitContent").RowSpan(2)
				cell("Fr(1)")
				cell("Fr(1)")
				cell("ColumnSpan(2)").ColumnSpan(2).JustifySelf(ui.Center)
			})
		})
		card(c, "Borders", func() {
			ui.Row(c).PaddingY(6).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
				ui.Text(c, "A header with a line below").Bold()
			})
			ui.Row(c).Padding(8, 12).Gap(8).BorderWidth(0, 0, 0, 4).BorderColor(t.Accent).Background(t.Surface).Radius(0, 6, 6, 0).Children(func() {
				ui.Text(c, "A note with an accent on its left")
			})
			ui.Column(c).Height(56).Radius(8).Border(2, t.Border).BorderStyle(ui.BorderDashed).Center().Children(func() {
				ui.Text(c, "Dashed, as a place to drop files").TextColor(t.TextMuted)
			})
		})
		card(c, "Fills", func() {
			blue, yellow := ui.Hex("#2563eb"), ui.Hex("#facc15")
			bar := func(label string) *ui.Element {
				return ui.Row(c).Height(30).PaddingX(10).Radius(6).Children(func() {
					ui.Text(c, label).FontSize(12).Bold().TextColor(ui.RGB(255, 255, 255))
				})
			}
			bar("sRGB").Gradient(blue, yellow, 90)
			bar("Oklab").LinearGradient(ui.LinearGradient{From: blue, To: yellow, Angle: 90, Oklab: true})
			bar("Stops at 40% and 60%").LinearGradient(ui.LinearGradient{From: blue, To: yellow, Angle: 90, Start: 0.4, End: 0.6})
			ui.Row(c).Height(30).Radius(6).Background(t.Surface).Stripes(t.Border, 4, 6, 45).Center().Children(func() {
				ui.Text(c, "Stripes: unavailable").FontSize(12).TextColor(t.TextMuted)
			})
		})
		card(c, "Text decorations", func() {
			ui.RichText(c, ui.Span{Text: "Spell checkers mark "}, ui.Span{Text: "mispeled", WavyUnderline: true, DecorationColor: t.Danger},
				ui.Span{Text: " words with waves."})
			ui.RichText(c, ui.Span{Text: "Search results "}, ui.Span{Text: "stand out", Background: ui.RGBA(250, 204, 21, 0.45)},
				ui.Span{Text: " with a background."})
			ui.Text(c, "Underlines of their own color and thickness").Underline().DecorationColor(t.Accent).DecorationThickness(2)
			ui.Text(c, "A highlight behind every line").TextBackground(t.Selection)
			ui.Text(c, strings.Repeat("A line cut with an ellipsis of its own. ", 3)).SingleLine().Ellipsis(" →")
		})
		card(c, "Motion", func() {
			ui.Row(c).Gap(14).Children(func() {
				spin := ui.Icon(c, loaderIcon).FontSize(24).TextColor(t.Accent)
				spin.Rotate(spin.Loop("spin", time.Second, ui.Linear) * 360)
				pulse := ui.Box(c).Height(14).Grow(1).Radius(7).Background(t.Border)
				pulse.Opacity(0.4 + 0.6*pulse.Loop("pulse", 1600*time.Millisecond, ui.Bounce(ui.EaseInOut)))
			})
			if ui.Button(c, "Move along each easing").Clicked() {
				g.eased = !g.eased
			}
			for _, e := range []struct {
				name string
				ease ui.Easing
			}{{"Linear", ui.Linear}, {"EaseIn", ui.EaseIn}, {"EaseOut", ui.EaseOut}, {"EaseInOut", ui.EaseInOut}} {
				track := ui.Row(c).Height(18).Key(e.name)
				to := float32(0)
				if g.eased {
					to = 1
				}
				at := track.AnimateWith("x", to, 900*time.Millisecond, e.ease)
				track.Children(func() {
					ui.Text(c, e.name).FontSize(12).Width(70).TextColor(t.TextMuted)
					ui.Box(c).Grow(1).Height(18).Children(func() {
						ui.Box(c).Size(18, 18).Radius(9).Background(t.Accent).Absolute().LeftPercent(at * 90)
					})
				})
			}
		})
		card(c, "Layout", func() {
			ui.Row(c).Gap(6).Reverse().Children(func() {
				for _, s := range []string{"1", "2", "3"} {
					ui.Box(c).Size(28, 28).Radius(6).Background(t.Surface).Center().Children(func() { ui.Text(c, s) })
				}
				ui.Text(c, "Reverse()").FontSize(12).TextColor(t.TextMuted).Margin(0, ui.Auto, 0, 0)
			})
			ui.Row(c).Gap(6).Children(func() {
				ui.Text(c, "Margin(…, Auto) pushes to the end").FontSize(12).TextColor(t.TextMuted)
				ui.Button(c, "Save").Margin(0, 0, 0, ui.Auto)
			})
			ui.Row(c).Gap(6).Children(func() {
				ui.Text(c, "Inbox")
				ui.Text(c, "3").FontSize(10).Bold().Padding(1, 5).Radius(8).Background(t.Danger).TextColor(ui.RGB(255, 255, 255)).Top(-6)
				ui.Text(c, "Top(-6) moves a badge up").FontSize(12).TextColor(t.TextMuted)
			})
		})
		card(c, "Scrolling both ways", func() {
			ui.ScrollBoth(c).Height(150).Radius(6).Border(1, t.Border).Children(func() {
				ui.Grid(c).ColumnTracks(repeat(ui.Fixed(56), 16)...).Gap(4).Padding(6).Children(func() {
					for i := range 16 * 12 {
						ui.Box(c).Height(28).Radius(4).Background(t.Accent.Alpha(0.08 + 0.6*float32(i%16)/16*float32(i/16)/12)).Center().Children(func() {
							ui.Textf(c, "%c%d", 'A'+i%16, i/16+1).FontSize(11)
						})
					}
				})
			})
		})
		card(c, "Cursors", func() {
			ui.Row(c).Wrap().Gap(6).Children(func() {
				for _, k := range []struct {
					name   string
					cursor ui.Cursor
				}{{"ResizeColumn", ui.CursorResizeColumn}, {"ResizeRow", ui.CursorResizeRow}, {"ResizeE", ui.CursorResizeE},
					{"Copy", ui.CursorCopy}, {"Alias", ui.CursorAlias}, {"ContextMenu", ui.CursorContextMenu},
					{"VerticalText", ui.CursorVerticalText}, {"None", ui.CursorNone}} {
					ui.Box(c).Padding(6, 10).Radius(6).Background(t.Surface).Cursor(k.cursor).Children(func() { ui.Text(c, k.name).FontSize(12) })
				}
			})
		})
	})
}

// repeat returns n tracks t.
func repeat(t ui.Track, n int) []ui.Track {
	out := make([]ui.Track, n)
	for i := range out {
		out[i] = t
	}
	return out
}

func (g *gallery) drawing(c *ui.Context) {
	t := c.Theme()
	ui.Text(c, "Element.Draw paints with rectangles, shadows, paths and text.").TextColor(t.TextMuted)
	ui.Box(c).Height(320).Radius(10).Background(t.Surface).Draw(func(p *ui.Painter, r ui.Rect) {
		phase := float64(c.Now().UnixMilli()%4000) / 4000 * 2 * math.Pi
		// Bars.
		for i := 0; i < 12; i++ {
			h := float32(60 + 50*math.Sin(phase+float64(i)*0.6))
			x := r.X + 24 + float32(i)*28
			p.Fill(ui.Rect{X: x, Y: r.Y + r.H - 24 - h, W: 18, H: h}, t.Accent.Alpha(0.35+0.05*float32(i)), 4)
		}
		// A sine wave.
		var wave ui.Path
		for i := 0; i <= 100; i++ {
			x := r.X + 380 + float32(i)*3
			y := r.Y + r.H/2 + float32(60*math.Sin(phase*2+float64(i)/12))
			if i == 0 {
				wave.MoveTo(x, y)
			} else {
				wave.LineTo(x, y)
			}
		}
		p.StrokePath(&wave, 3, t.Danger)
		var dot ui.Path
		dot.Circle(r.X+r.W-70, r.Y+70, 36)
		p.FillPath(&dot, t.Accent)
		p.Text(r.X+24, r.Y+20, "Animated at the display's rate", 14, t.Text)
	})
	c.AnimationFrame()
	card(c, "Vector images", func() {
		ui.Text(c, "SVGs stay sharp at any size: icons in the color of the text, pictures in their own colors.").TextColor(t.TextMuted)
		gold := ui.RGB(245, 180, 0)
		ui.Row(c).Gap(14).Children(func() {
			for _, size := range []float32{16, 24, 40} {
				ui.Icon(c, starIcon).FontSize(size).TextColor(gold)
			}
			// A button lays out its children in a row.
			done := ui.PrimaryButton(c, "").Children(func() {
				ui.Icon(c, checkIcon)
				ui.Text(c, "Done").SingleLine()
			})
			if done.Clicked() {
				c.Toast("Done")
			}
			ui.Image(c, badge).Size(64, 64).TextColor(gold)
			ui.Image(c, badge).Size(32, 32).TextColor(gold)
		})
	})
}

func (g *gallery) overlays(c *ui.Context) {
	t := c.Theme()
	card(c, "Overlays", func() {
		ui.Row(c).Gap(10).Children(func() {
			if ui.PrimaryButton(c, "Open dialog").Clicked() {
				g.dialog = true
			}
			menu := ui.Button(c, "Menu ▾")
			if menu.Clicked() {
				g.menu = !g.menu
			}
			ui.Popover(c, menu, &g.menu, func() {
				for _, item := range []string{"New file", "Open…", "Save as…"} {
					entry := ui.Row(c).Key(item).Padding(6, 12).Radius(5).Width(180)
					if entry.Hovered() {
						entry.Background(t.Accent).TextColor(t.AccentText)
					}
					if entry.Clicked() {
						g.menu = false
					}
					entry.Children(func() { ui.Text(c, item) })
				}
			})
			ui.Button(c, "Hover me").Tooltip("Tooltips show after the pointer rests a moment.")
			if ui.Button(c, "Native dialog").Clicked() {
				go mygo.Dialog.Message(mygo.MessageOptions{Parent: g.win, Message: "Native dialogs work from MyGo UI windows too."})
			}
		})
	})
	ui.Modal(c, &g.dialog, func() {
		ui.Text(c, "A modal dialog").FontSize(18).Bold()
		ui.Text(c, "Click outside or press Escape to close it.").TextColor(t.TextMuted)
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.PrimaryButton(c, "Done").Clicked() {
				g.dialog = false
			}
		})
	})
}

func main() {
	g := &gallery{page: "Overview", size: "Medium", fruit: "Apple", plan: "Pro", volume: 35, picked: -1, starred: map[int]bool{}, split: 160, copies: 1, tree: map[string]bool{"ui": true}, birthday: time.Date(1815, 12, 10, 0, 0, 0, 0, time.UTC), now: time.Now()}
	mygo.App.WhenReady(func() {
		g.win = mygo.NewWindow(mygo.WindowOptions{
			Title:    "MyGo UI Gallery",
			Width:    980,
			Height:   720,
			MinWidth: 640, MinHeight: 480,
			StateKey: "gallery",
			Content:  ui.View(g.view),
		})
		go func() {
			x, sample := 0.0, func(x float64) float64 { return 0.5 + 0.35*math.Sin(x) + 0.1*math.Sin(x*3.1) }
			// A minute of samples to start with, then one a second, when
			// the clock changes: the window draws nothing in between.
			history := make([]float64, 60)
			for i := range history {
				x += 0.35
				history[i] = sample(x)
			}
			g.win.Update(func() { g.samples = history })
			for {
				time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second)))
				x += 0.35
				v := sample(x)
				g.win.Update(func() {
					g.now = time.Now()
					g.samples = append(g.samples[1:], v)
				})
			}
		}()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
