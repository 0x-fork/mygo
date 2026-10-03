package ui

import "time"

// toastTime is how long a toast shows.
const toastTime = 4 * time.Second

// toast is a message the window shows for a while.
type toast struct {
	message string
	at      time.Time
	id      uint64
}

// Toast shows message near the bottom of the window for a few seconds, as
// the outcome of what the user just did:
//
//	if ui.Button(c, "Save").Clicked() {
//		app.save()
//		c.Toast("Saved")
//	}
//
// A message already showing shows anew; others stack above it.
func (c *Context) Toast(message string) {
	rt := c.rt
	for i, ts := range rt.toasts {
		if ts.message == message {
			rt.toasts = append(rt.toasts[:i], rt.toasts[i+1:]...)
			break
		}
	}
	rt.nextToast++
	rt.toasts = append(rt.toasts, toast{message: message, at: c.now, id: rt.nextToast})
	rt.requestFrame()
}

// buildToasts builds the toasts showing, in the overlay, and forgets those
// that ended.
func (rt *engine) buildToasts(c *Context) {
	live := rt.toasts[:0]
	for _, ts := range rt.toasts {
		if c.now.Sub(ts.at) < toastTime {
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
			for _, ts := range live {
				age := c.now.Sub(ts.at)
				box := Row(c).Key(ts.id).Padding(t.Space(2.5), t.Space(4)).Radius(t.Space(2)).MaxWidth(c.w - t.Space(12)).
					Background(t.Text).TextColor(t.Background).Role(RoleStatus)
				box.Shadow(0, 6, 20, 0, RGBA(0, 0, 0, 0.25))
				// It fades in, and out at the end.
				const fade = 200 * time.Millisecond
				opacity := float32(1)
				switch left := toastTime - age; {
				case age < fade:
					opacity = float32(age) / float32(fade)
					c.AnimationFrame()
				case left < fade:
					opacity = float32(left) / float32(fade)
					c.AnimationFrame()
				default:
					c.After(left - fade)
				}
				box.Opacity(opacity)
				box.Children(func() { Text(c, ts.message) })
			}
		})
	})
}
