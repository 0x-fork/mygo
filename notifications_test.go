package mygo

import (
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// showNotification shows a notification for a test. The fake backend
// records it by its id, which is how a test sees what the platform would
// still be showing.
func showNotification(t *testing.T, opts NotificationOptions) *Notification {
	t.Helper()
	n := NewNotification(opts)
	if err := n.Show(); err != nil {
		t.Fatalf("Show: %v", err)
	}
	t.Cleanup(n.Close)
	return n
}

// clickNotification reports a click on a notification as a backend does,
// on the main thread, where the platform's delegate reports it.
func clickNotification(t *testing.T, n *Notification) {
	t.Helper()
	onMain(func() { fb.ClickNotification(n.id) })
}

// TestNotificationCloseOnClick: a click reaches the notification's
// listeners, which may Close it, as macOS keeps it otherwise, and show
// another, as answering a message does, which stays.
func TestNotificationCloseOnClick(t *testing.T) {
	first := showNotification(t, NotificationOptions{Title: "first"})
	other := showNotification(t, NotificationOptions{Title: "other"})
	clicks := 0
	var second *Notification
	first.OnClick(func() {
		clicks++
		first.Close()
		second = NewNotification(NotificationOptions{Title: "second"})
		if err := second.Show(); err != nil {
			t.Errorf("Show: %v", err)
		}
	})
	clickNotification(t, first)
	if second == nil {
		t.Fatal("the listener did not run")
	}
	t.Cleanup(second.Close)
	if fb.Notification(first.id) != nil {
		t.Error("the notification closed in its listener is still shown")
	}
	if fb.Notification(second.id) == nil || fb.Notification(other.id) == nil {
		t.Error("a notification the click did not close was removed")
	}
	// A click on one that was closed reaches nobody.
	clickNotification(t, first)
	if clicks != 1 {
		t.Errorf("%d clicks, want 1", clicks)
	}
}

// TestClearNotifications: every notification of the app is removed,
// those of an earlier run that only the platform knows too, and a click
// reaches none of them afterwards.
func TestClearNotifications(t *testing.T) {
	one := showNotification(t, NotificationOptions{Title: "one"})
	two := showNotification(t, NotificationOptions{Title: "two"})
	closed := showNotification(t, NotificationOptions{Title: "closed"})
	closed.Close()
	earlier := &platform.Notification{ID: "earlier-run", Title: "earlier"}
	onMain(func() { fb.Delivered(earlier) })

	clicked := 0
	for _, n := range []*Notification{one, two, closed} {
		n.OnClick(func() { clicked++ })
	}

	ClearNotifications()

	if left := fb.Notifications(); len(left) != 0 {
		t.Errorf("%d notifications left after ClearNotifications", len(left))
	}
	clickNotification(t, one)
	clickNotification(t, two)
	if clicked != 0 {
		t.Errorf("%d listeners ran after ClearNotifications, want 0", clicked)
	}

	// Clearing again, with nothing shown, is not an error.
	ClearNotifications()
}

// TestNotificationOptionsReachTheBackend checks that Show passes the
// options on.
func TestNotificationOptionsReachTheBackend(t *testing.T) {
	n := showNotification(t, NotificationOptions{
		Title: "title", Subtitle: "subtitle", Body: "body",
		Silent: true,
	})
	got := fb.Notification(n.id)
	if got == nil {
		t.Fatal("the notification did not reach the backend")
	}
	if got.Title != "title" || got.Subtitle != "subtitle" || got.Body != "body" {
		t.Errorf("got %q/%q/%q, want title/subtitle/body", got.Title, got.Subtitle, got.Body)
	}
	if !got.Silent {
		t.Error("Silent = false, want true")
	}
}
