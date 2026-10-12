# Game

The game plugin (`plugins/game`) has what games need beyond what apps do:
MyGo is an app framework, and these are opt-in, in a package of their
own. It is all Go.

## Gamepads

Package `plugins/game/gamepad` reads game controllers, in apps of native
UI or web pages, on every platform MyGo runs on, browsers included. Every
pad reads in one layout, the standard one of the W3C's Gamepad API and
SDL's game controller database, whatever pad it is: buttons by place, two
sticks, analog triggers and a d-pad.

```go
import "github.com/egoist/mygo/plugins/game/gamepad"

var last gamepad.State

func update() {
	pads := gamepad.Pads()
	if len(pads) == 0 {
		return
	}
	s := pads[0].State()
	x, y := s.LeftStick() // -1 to 1, right and down, with a dead zone
	player.move(x, y)
	if s.JustPressed(last, gamepad.South) {
		player.jump()
	}
	last = s
}
```

### Pads

`Pads` returns the pads connected, in the order they connected, and looks
for new ones at most once a second; a game calls it once a frame. A
`Pad` lasts from when it connects until it disconnects (`Connected` turns
false, and it leaves `Pads`); plugged in again, it is a new one. `Name` is
what its maker calls it.

`State` reads a pad's controls now, as a value: keep the last one to
tell a button just pressed (`JustPressed`) or released (`JustReleased`)
from one held.

### The layout

| button | Xbox | PlayStation | Nintendo |
|---|---|---|---|
| `South` | A | Cross | B |
| `East` | B | Circle | A |
| `West` | X | Square | Y |
| `North` | Y | Triangle | X |
| `LeftShoulder`, `RightShoulder` | LB, RB | L1, R1 | L, R |
| `LeftTrigger`, `RightTrigger` | LT, RT | L2, R2 | ZL, ZR |
| `Back`, `Start` | View, Menu | Create, Options | −, + |
| `LeftStickButton`, `RightStickButton` | LS, RS | L3, R3 | |
| `Up`, `Down`, `Left`, `Right` | d-pad | d-pad | d-pad |
| `Guide` | Xbox | PS | Home |

Buttons read from 0 to 1 (`Value`), the triggers by how far they are
pulled; `Pressed` is a quarter of the way. Axes (`Axis`) read from -1 to 1,
right and down positive, as the pad reports them; `LeftStick` and
`RightStick` apply a radial dead zone (`Deadzone`, 0.15), so that a stick
at rest reads nothing and leans as far in every direction.

### Platforms

| platform | from | pads |
|---|---|---|
| macOS | GameController.framework | Xbox, PlayStation, Switch Pro and MFi controllers |
| Windows | XInput | Xbox controllers and those that act as one (Steam Input makes others so); Guide through `XInputGetStateEx` |
| Linux | evdev, `/dev/input/event*` | what the kernel's drivers report as gamepads, readable by the user at the seat; Xbox controllers' X and Y, reported as BTN_X and BTN_Y, read in place |
| browsers | the Gamepad API | those with the standard mapping; others in the order the browser gives |

On macOS, a pad's input reaches the app in front only: in the background
it reads nothing pressed, as games expect. Browsers report a pad only
once the page saw one of its buttons pressed.
Windows sees no PlayStation or Nintendo controller without Steam Input or
a driver that makes it an XInput one.

[examples/farm](../../examples/farm) plays with a gamepad as with the
keyboard.
