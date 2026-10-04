package ui

import "time"

// toastTime is how long a toast shows, and actionToastTime one with an
// action, which the user needs time to reach.
const (
	toastTime       = 4 * time.Second
	actionToastTime = 8 * time.Second
)

// toast is a message the window shows for a while: since at, less the
// time the pointer rested on it, from pausedAt while it does, with the
// button of an action.
type toast struct {
	message  string
	at       time.Time
	pausedAt time.Time
	id       uint64
	label    string
	action   func()
}

// life is how long the toast shows.
func (ts *toast) life() time.Duration {
	if ts.action != nil {
		return actionToastTime
	}
	return toastTime
}

// Toast shows message near the bottom of the window for a few seconds, as
// the outcome of what the user just did:
//
//	if ui.Button(c, "Save").Clicked() {
//		app.save()
//		c.Toast("Saved")
//	}
//
// A message already showing shows anew; others stack above it. It shows
// for as long as the pointer rests on it. Screen readers read it out.
func (c *Context) Toast(message string) {
	c.toast(toast{message: message})
}

// ToastAction shows message as Toast does, for longer, with a button of
// label that runs action, as Undo after deleting, and closes the toast;
// Tab reaches the button. action runs on the main thread, as the view
// does, which builds the frame again after it.
//
//	app.trash(note)
//	c.ToastAction("Note deleted", "Undo", func() { app.restore(note) })
func (c *Context) ToastAction(message, label string, action func()) {
	c.toast(toast{message: message, label: label, action: action})
}

func (c *Context) toast(ts toast) {
	rt := c.rt
	for i, old := range rt.toasts {
		if old.message == ts.message {
			rt.toasts = append(rt.toasts[:i], rt.toasts[i+1:]...)
			break
		}
	}
	rt.nextToast++
	ts.at, ts.id = c.now, rt.nextToast
	rt.toasts = append(rt.toasts, ts)
	// Nothing else tells screen readers: the focus stays.
	c.Announce(ts.message)
	rt.requestFrame()
}

// buildToasts builds the toasts showing, in the overlay, and forgets those
// that ended.
func (rt *engine) buildToasts(c *Context) {
	age := func(ts *toast) time.Duration {
		if !ts.pausedAt.IsZero() {
			return ts.pausedAt.Sub(ts.at)
		}
		return c.now.Sub(ts.at)
	}
	live := rt.toasts[:0]
	for _, ts := range rt.toasts {
		if age(&ts) < ts.life() {
			live = append(live, ts)
		}
	}
	rt.toasts = live
	if len(live) == 0 {
		return
	}
	t := c.theme
	Overlay(c, func() {
		stack := Column(c).Absolute().Left(0).Right(0).Bottom(t.Space(6)).AlignItems(Center).Gap(t.Space(2)).PassThrough()
		stack.Children(func() {
			for i := range live {
				ts := &rt.toasts[i]
				box := Row(c).Key(ts.id).AlignItems(Center).Gap(t.Space(4)).Padding(t.Space(2.5), t.Space(4)).Radius(t.Space(2)).MaxWidth(c.w - t.Space(12)).
					Background(t.Text).TextColor(t.Background).Role(RoleStatus)
				box.Shadow(0, 6, 20, 0, RGBA(0, 0, 0, 0.25))
				// The time stops while the pointer rests on it.
				switch hovered := box.Hovered(); {
				case hovered && ts.pausedAt.IsZero():
					ts.pausedAt = c.now
				case !hovered && !ts.pausedAt.IsZero():
					ts.at = ts.at.Add(c.now.Sub(ts.pausedAt))
					ts.pausedAt = time.Time{}
				}
				a := age(ts)
				// It fades in, and out at the end.
				const fade = 200 * time.Millisecond
				opacity := float32(1)
				switch left := ts.life() - a; {
				case !ts.pausedAt.IsZero():
				case a < fade:
					opacity = float32(a) / float32(fade)
					c.AnimationFrame()
				case left < fade:
					opacity = float32(left) / float32(fade)
					c.AnimationFrame()
				default:
					c.After(left - fade)
				}
				box.Opacity(opacity)
				box.Children(func() {
					Text(c, ts.message)
					if ts.action == nil {
						return
					}
					b := ButtonBase(c).Padding(t.Space(1), t.Space(2.5)).Radius(t.Radius).TextColor(t.Accent.Mix(t.Background, 0.35)).FontWeight(600)
					b.styleFn = func(b *Element) {
						if b.Hovered() {
							b.bg = t.Background.Alpha(0.12)
						}
					}
					b.Children(func() { Text(c, ts.label) })
					if b.Clicked() {
						// It ends, as the next pass forgets it.
						action := ts.action
						ts.at, ts.pausedAt, ts.action = time.Time{}, time.Time{}, nil
						rt.consumed = true
						action()
					}
				})
			}
		})
	})
}
