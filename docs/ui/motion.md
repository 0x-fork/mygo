# Motion

<!-- preview motion 220 -->

An element animates values for you: you give the target, it returns the
value to show in this frame, and draws frames until the value gets there.
The view says where things are; the animation finds the way:

```go
card := ui.Box(c).Size(120, 80).Radius(12).Background(t.Accent)
target := float32(0)
if app.open {
	target = 200
}
card.Translate(card.AnimateTo("x", target, ui.Snappy), 0)
```

The key (`"x"`) tells apart the values of one element, which needs the same
ID from frame to frame: a `Key`, or the same place among its siblings. A
value starts at its first target. Use the value in any way: a transform
(see [Transforms](transforms.md)), a size, an opacity, a color mix, or what
a `Draw` paints.

## Animations

An `Animation` says how a value goes to a new target:

- **Springs**, the zero `Animation` and the presets `ui.Smooth` (no
  bounce), `ui.Snappy` (a little) and `ui.Bouncy` (a visible one). A
  spring keeps its velocity when its target changes on the way: clicked
  again mid-flight, the card above turns around smoothly instead of
  stopping dead.
- **Curves**: a `Duration` and an `Ease` (`ui.EaseOut` when nil), as
  `ui.Animation{Duration: 150 * time.Millisecond, Ease: ui.EaseInOut}`.
  `Animate(key, target, d)` and `AnimateWith(key, target, d, ease)` are
  short for them.

A spring's `SpringOptions` describe it as one perceives it, with a
`Duration` (500 ms when zero), about how long it takes to arrive, and a
`Bounce`, from 0, arriving without overshooting, toward 1; negative values
arrive more slowly still. These are SwiftUI's and Motion's parameters.
`Stiffness`, `Damping` and `Mass` describe it physically instead:

```go
gentle := ui.Animation{Spring: ui.SpringOptions{Duration: 700 * time.Millisecond, Bounce: 0.1}}
stiff := ui.Animation{Spring: ui.SpringOptions{Stiffness: 400, Damping: 30}}
x := card.AnimateTo("x", target, gentle)
```

`Spring(key, target, options)` is short for `AnimateTo` with a spring.
Springs are solved exactly rather than stepped, so a slow frame does not
change their path. A value can change its kind of animation on the way: a
spring taking over a curve keeps the curve's velocity.

`Easing` functions are `ui.Linear`, `ui.EaseIn`, `ui.EaseOut`,
`ui.EaseInOut`, `ui.Bounce(e)`, CSS's `ui.CubicBezier(x1, y1, x2, y2)`, or
any `func(t float32) float32`.

`AnimateColor(key, target, a)` animates a color the same way, mixed with
premultiplied alpha, so that a color fading in from transparent keeps its
hue:

```go
row.Background(row.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond}))
```

## Poses that only paint

The values above are read by the view, so every frame that moves runs it.
`AnimatePose` moves an element instead: it translates, scales, turns and
fades it, after its own transform, and the frames that move it only paint,
without running the view, as Core Animation and the Web Animations API run
transforms and opacity. It is the cheapest motion there is, which keeps a
large view smooth:

```go
lift := ui.Pose{}
if card.Hovered() {
	lift = ui.Pose{Y: -4, Scale: 1.02}
}
card.AnimatePose(lift, ui.Snappy)
```

A `Pose` moves by `X` and `Y`, scales by `Scale` (0 keeps the size),
turns by `Rotate` degrees, and fades by `Fade`, from 0, opaque, to 1, gone.
The pointer, carets and overlays follow the element as it moves, and
`Transitioning()` reports whether its pose was moving.

## Delays and staggering

`Delay` keeps the value going as it was for a while after its target
changes. Delays growing with the index stagger a list:

```go
for i, it := range app.items {
	row := ui.Row(c.Key(it.ID))
	a := ui.Snappy
	a.Delay = time.Duration(i) * 30 * time.Millisecond
	row.Opacity(row.AnimateTo("shown", 1, a))
}
```

## Gestures

A value can follow the pointer and then be let go. `Jump` sets it at once,
following the pointer, and keeps track of how fast it moves; the spring
that takes over when the pointer lets go starts at that speed.
`DragEnded` reports the pointer's velocity as it let go, which
`SpringOptions.Velocity` hands to the spring, to throw things:

```go
card := ui.Box(c).Size(90, 60)
var x float32
if dx, _, ok := card.Dragged(); ok {
	app.x += dx
	x = card.Jump("x", app.x)
} else {
	a := ui.Snappy
	if vx, _, ok := card.DragEnded(); ok {
		// Where the throw would carry it decides where it lands.
		app.right = app.x+vx*0.2 > 100
		a.Spring.Velocity = vx
	}
	target := float32(0)
	if app.right {
		target = 200
	}
	x = card.AnimateTo("x", target, a)
	app.x = x
}
card.Translate(x, 0)
```

`DragVelocity` is the pointer's velocity while it presses, in DIPs per
second along the element's axes, zero once it rests a moment. `Velocity(key)`
is the velocity of an animated value. A click, which does not move, ends no
drag.

`ui.Project(value, velocity)` is where a fling would carry a value as
iOS's scroll views slow down, to choose where it lands, as a page of a
pager. `ui.Rubberband(value, lo, hi, dimension)` holds a value dragged past
lo or hi as iOS holds content pulled past its edges, going less and less
far:

```go
if _, dy, ok := sheet.Dragged(); ok {
	app.y += dy
	y = sheet.Jump("y", ui.Rubberband(app.y, 0, 400, 600))
}
```

## When it is done

`Settled(key)` reports whether a value rests at its target, and
`Arrived(key)` is true in the one frame in which it came to rest, for the
view to go on, as removing what faded out:

```go
toast.Opacity(toast.AnimateTo("fade", 0, ui.Animation{Duration: 200 * time.Millisecond}))
if toast.Arrived("fade") {
	app.dismiss(toast)
}
```

## Without animation

`c.DisableAnimations()` makes the values, transitions, poses and router
pages of the frame go to their targets at once, as SwiftUI's
disablesAnimations: for a change that should not show as motion, as
restoring a window's state or jumping to a search result. Call it before
building what changes.

## Keyframes

`Keyframes` plays timed values, each segment along the `Ease` of the frame
it arrives at, and holds the last:

```go
tile := ui.Box(c).Key("tile").Size(80, 50)
x := tile.Keyframes("x", []ui.Keyframe{
	{Value: 0},
	{At: 200 * time.Millisecond, Value: 100, Ease: ui.EaseOut},
	{At: 700 * time.Millisecond, Value: 40},
	{At: time.Second, Value: 0, Ease: ui.EaseInOut},
}, ui.KeyframeOptions{Run: app.shakes})
tile.Translate(x, 0)
```

The first time must be zero and later times must increase. Change `Run` to
play the sequence again, or a changed one: interrupted, it starts from the
value shown rather than jumping to its first, the first time through.
`Repeat` is how many more times it plays after the first, `RepeatForever`
for as long as it is built, with `Alternate` playing every other time
backward; `Delay` holds the first value for a while before it plays, and
`RepeatDelay` the value it got to between plays.

## Scrolling

`InView()` reports whether some of an element showed in the frame before,
within the scroll containers around it, to bring it in as it scrolls into
view. A `ScrollState` (see [Scrolling](scroll.md)) gives how far its
container scrolled, for effects that follow the scrolling:

```go
card := ui.Box(c).Key(it.ID).Height(120)
shown := float32(0)
if card.InView() {
	shown = 1
}
card.Opacity(card.AnimateTo("shown", shown, ui.Smooth))

header.Opacity(1 - min(app.scroll.Y/80, 1)) // fades as the page scrolls
```

`ScrollState.ScrollTo(x, y, animation)` scrolls a container smoothly (see
[Scrolling](scroll.md)).

## Content that changes

`ContentTransition` animates an element's content as it changes: the text
of a `Text`, the icon of an `Icon`, the picture of an `Image`. By default
the old content fades out as the new fades in, and an icon shrinks away as
the new one grows in. `Numeric` rolls the characters of a line of text that
change, as a counter's digits, those that stay staying: up, or with `Down`
down, as SwiftUI's numericText:

```go
ui.Text(c, strconv.Itoa(app.count)).FontSize(32).Bold().
	ContentTransition(ui.ContentTransition{Numeric: true, Down: app.decreased})
ui.Icon(c, playOrPause).ContentTransition(ui.ContentTransition{})
```

Content transitions run in frames that only paint. While the desktop asks
for less motion, content fades without moving.

## Loops and your own motion

`Loop` returns the progress of an animation that starts over every period,
for spinners and pulses (see [Drawing and animation](drawing.md#animation)).
To animate otherwise, compute from `c.Now()` and call `c.AnimationFrame()`
in every frame that moves.

## Less motion and frames

When the desktop asks for less motion (`c.Preferences().ReduceMotion`),
values and poses go to their targets at once and sequences end at their
last value. What changes without moving still changes smoothly: colors
(`AnimateColor`), a pose's `Fade`, content fading across, and
[transitions](transitions.md)' colors and fades. `Loop` goes on, as the
system's spinners do.

Animations draw frames only while something moves, at the display's rate,
and none while other windows cover the window. Frames moving values the
view reads run the view; poses, content transitions, and drawings that
animate from their `Draw` only paint.
