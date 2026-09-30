package updater

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/egoist/mygo"
)

// session is a check for updates and what follows it: the update window,
// the download, the offer to relaunch. The window shows the session's
// current view, which the session keeps up to date even while the window
// is not shown, so that it can show at any point.
type session struct {
	u *updater
	// ctx is canceled when the session ends, the user cancels or the
	// window closes.
	ctx       context.Context
	cancel    context.CancelFunc
	responses chan response

	// showMu is held while the window is being created.
	showMu sync.Mutex

	mu sync.Mutex
	// user is set when the user asked for the check: the window shows
	// from the start and reports that the app is up to date.
	user    bool
	shown   bool
	ended   bool
	view    view
	changed chan struct{} // closed when the view changes
	win     *mygo.Window
	release bool // the window has the size of a release view
}

func newSession(u *updater, user bool) *session {
	ctx, cancel := context.WithCancel(context.Background())
	return &session{
		u: u, user: user, ctx: ctx, cancel: cancel,
		responses: make(chan response, 1),
		changed:   make(chan struct{}),
	}
}

func (s *session) isUser() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.user
}

func (s *session) isShown() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shown
}

// current returns the view and a channel closed when it changes.
func (s *session) current() (view, <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.view, s.changed
}

// set changes the view, and the window's size when the view needs another.
func (s *session) set(v view) {
	s.mu.Lock()
	v.Prompt = s.view.Prompt
	if !slices.Equal(v.Buttons, s.view.Buttons) {
		v.Prompt++
	}
	s.view = v
	close(s.changed)
	s.changed = make(chan struct{})
	win := s.win
	resize := win != nil && s.release != v.Release
	if resize {
		s.release = v.Release
	}
	s.mu.Unlock()
	if resize {
		fit(win, v.Release)
	}
}

// show shows the window, bringing it to the front when the user asked for
// the check.
func (s *session) show() {
	s.showMu.Lock()
	defer s.showMu.Unlock()
	s.mu.Lock()
	win, user, ended, shown := s.win, s.user, s.ended, s.shown
	s.shown = shown || !ended
	s.mu.Unlock()
	switch {
	case ended:
	case !shown:
		present(s)
	case user && win != nil:
		win.Show()
		win.Focus()
	}
}

// promote makes a check running in the background one the user asked for.
func (s *session) promote() {
	s.mu.Lock()
	s.user = true
	s.mu.Unlock()
	s.show()
}

// wait waits for the user to respond to the view, or the window to close.
func (s *session) wait() response {
	select {
	case r := <-s.responses:
		return r
	case <-s.ctx.Done():
		return response{action: actionClose}
	}
}

// respond takes the user's response to the prompt of a view. Only one
// response to a prompt counts, and only with one of its buttons.
func (s *session) respond(prompt int, a action, automaticDownloads bool) {
	s.mu.Lock()
	ok := prompt == s.view.Prompt && slices.ContainsFunc(s.view.Buttons, func(b button) bool {
		return b.Action == a && !b.Disabled
	})
	if ok {
		s.view.Prompt++
	}
	s.mu.Unlock()
	switch {
	case !ok:
	case a == actionCancel:
		s.cancel()
	default:
		select {
		case s.responses <- response{a, automaticDownloads}:
		default: // the session ended
		}
	}
}

// close ends the session and closes its window.
func (s *session) close() {
	s.cancel()
	s.showMu.Lock()
	s.mu.Lock()
	s.ended = true
	win := s.win
	s.mu.Unlock()
	s.showMu.Unlock()
	if win != nil {
		win.Close()
	}
}

// action is a button of the update window.
type action string

const (
	actionOK       action = "ok"
	actionCancel   action = "cancel"
	actionSkip     action = "skip"
	actionLater    action = "later"
	actionInstall  action = "install"
	actionRelaunch action = "relaunch"
	// actionClose is the window closing, not a button.
	actionClose action = "close"
)

type response struct {
	action             action
	automaticDownloads bool
}

// view is what the update window shows.
type view struct {
	// Prompt changes with the buttons: responses name the prompt they
	// answer, so that none answers a later one.
	Prompt int `json:"prompt"`
	// Release views show release notes, in a larger window.
	Release bool   `json:"release,omitzero"`
	Title   string `json:"title"`
	Message string `json:"message,omitzero"`
	// Detail is the error of a failure.
	Detail string `json:"detail,omitzero"`
	// Notes are the release notes, in HTML.
	Notes string `json:"notes,omitzero"`
	// Bar shows a progress bar with Progress, between 0 and 1, or -1 when
	// the progress is unknown.
	Bar      bool    `json:"bar,omitzero"`
	Progress float64 `json:"progress,omitzero"`
	// Checkbox offers to download and install updates automatically,
	// Checked or not.
	Checkbox bool     `json:"checkbox,omitzero"`
	Checked  bool     `json:"checked,omitzero"`
	Buttons  []button `json:"buttons"`
}

type button struct {
	Action action `json:"action"`
	Label  string `json:"label"`
	// Default is the button of Enter, Cancel the one of Escape.
	Default bool `json:"default,omitzero"`
	Cancel  bool `json:"cancel,omitzero"`
	// Aside sets the button apart, on the left.
	Aside    bool `json:"aside,omitzero"`
	Disabled bool `json:"disabled,omitzero"`
}

func (t *text) ok() []button {
	return []button{{Action: actionOK, Label: t.OK, Default: true, Cancel: true}}
}

func (t *text) cancel(disabled bool) []button {
	return []button{{Action: actionCancel, Label: t.Cancel, Cancel: true, Disabled: disabled}}
}

func (t *text) checkingView() view {
	return view{Title: t.Checking, Bar: true, Progress: -1, Buttons: t.cancel(false)}
}

func (t *text) upToDateView() view {
	return view{
		Title:   t.UpToDate,
		Message: fmt.Sprintf(t.UpToDateMessage, mygo.App.Name(), appVersion()),
		Buttons: t.ok(),
	}
}

func (t *text) unavailableView() view {
	msg := fmt.Sprintf(t.UnavailableMessage, mygo.App.Name())
	if mygo.IsDev() {
		msg = t.DevelopmentBuild
	}
	return view{Title: t.Unavailable, Message: msg, Buttons: t.ok()}
}

func (t *text) errorView(msg string, err error) view {
	return view{Title: t.Error, Message: msg, Detail: err.Error(), Buttons: t.ok()}
}

func (t *text) availableView(r *release, automaticDownloads bool) view {
	name := mygo.App.Name()
	notes := renderMarkdown(r.notes)
	return view{
		Release:  notes != "",
		Title:    fmt.Sprintf(t.Available, name),
		Message:  fmt.Sprintf(t.AvailableMessage, name, r.version, appVersion()),
		Notes:    notes,
		Checkbox: true,
		Checked:  automaticDownloads,
		Buttons: []button{
			{Action: actionSkip, Label: t.Skip, Aside: true},
			{Action: actionLater, Label: t.RemindLater, Cancel: true},
			{Action: actionInstall, Label: t.Install, Default: true},
		},
	}
}

func (t *text) downloadingView(downloaded, total int64) view {
	v := view{Title: t.Downloading, Bar: true, Progress: -1, Buttons: t.cancel(false)}
	if total > 0 {
		v.Progress = float64(downloaded) / float64(total)
		v.Message = fmt.Sprintf(t.Progress, t.megabytes(downloaded), t.megabytes(total))
	}
	return v
}

func (t *text) installingView() view {
	return view{Title: t.Installing, Bar: true, Progress: -1, Buttons: t.cancel(true)}
}

func (t *text) readyView(r *release) view {
	return view{
		Title:   t.Ready,
		Message: fmt.Sprintf(t.ReadyMessage, mygo.App.Name(), r.version),
		Buttons: []button{
			{Action: actionLater, Label: t.Later, Cancel: true},
			{Action: actionRelaunch, Label: t.Relaunch, Default: true},
		},
	}
}
