package mygo

import (
	"testing"
	"time"
)

func TestPower(t *testing.T) {
	var got []string
	record := func(event string) func() { return func() { got = append(got, event) } }
	offs := []func(){
		Power.OnSuspend(record("suspend")),
		Power.OnResume(record("resume")),
		Power.OnLockScreen(record("lock")),
		Power.OnUnlockScreen(record("unlock")),
	}
	defer func() {
		for _, off := range offs {
			off()
		}
	}()
	deadline := time.Now().Add(time.Second)
	for !onMainValue(func() bool { return fb.Watching }) {
		if time.Now().After(deadline) {
			t.Fatal("the backend was not asked to watch power events")
		}
		time.Sleep(5 * time.Millisecond)
	}
	onMain(func() {
		for _, e := range []string{"suspend", "resume", "lock-screen", "unlock-screen", "unknown"} {
			fb.EmitPower(e)
		}
	})
	if want := "suspend resume lock unlock"; join(got) != want {
		t.Errorf("events = %q, want %q", join(got), want)
	}

	release := Power.KeepAwake("testing", true)
	if n := onMainValue(func() int { return fb.Awake }); n != 1 {
		t.Errorf("awake = %d", n)
	}
	release()
	release() // once
	if n := onMainValue(func() int { return fb.Awake }); n != 0 {
		t.Errorf("awake after release = %d", n)
	}
	if Power.IsOnBattery() || Power.IdleTime() != 42*time.Second {
		t.Error("IsOnBattery or IdleTime")
	}
}

func join(s []string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += " "
		}
		out += x
	}
	return out
}
