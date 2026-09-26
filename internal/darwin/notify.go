//go:build darwin

package darwin

import (
	"sync"

	"github.com/ebitengine/purego/objc"

	"github.com/egoist/mygo/internal/platform"
)

// Notifications use UNUserNotificationCenter, which only works for apps
// running from a bundle with an identifier.

var (
	mainQueueMu sync.Mutex
	mainQueue   []func()
)

// runOnMain runs fn on the main thread; used by callbacks the system
// delivers on background queues.
func (b *Backend) runOnMain(fn func()) {
	if b.IsMainThread() {
		fn()
		return
	}
	mainQueueMu.Lock()
	mainQueue = append(mainQueue, fn)
	mainQueueMu.Unlock()
	b.Signal()
}

func drainMainQueue() {
	mainQueueMu.Lock()
	q := mainQueue
	mainQueue = nil
	mainQueueMu.Unlock()
	for _, fn := range q {
		fn()
	}
}

func registerNotificationDelegate() {
	if !hasClass("UNUserNotificationCenter") {
		return
	}
	classDef("MyGoNotificationDelegate", "NSObject", []string{"UNUserNotificationCenterDelegate"}, []objc.MethodDef{
		method("userNotificationCenter:willPresentNotification:withCompletionHandler:", func(self id, _ objc.SEL, center, n id, handler uintptr) {
			// Show banners even while the app is in the foreground.
			callBlock(handler, 1<<1|1<<3|1<<4) // sound | list | banner
		}),
		method("userNotificationCenter:didReceiveNotificationResponse:withCompletionHandler:", func(self id, _ objc.SEL, center, resp id, handler uintptr) {
			ident := goString(send(send(send(send(resp, "notification"), "request"), "identifier"), "self"))
			theBackend.runOnMain(func() { theBackend.h.NotificationClicked(ident) })
			callBlock(handler)
		}),
	})
}

func (b *Backend) NotificationsSupported() bool {
	_, packaged := appController{b}.Package()
	return packaged && hasClass("UNUserNotificationCenter")
}

func (b *Backend) notificationCenter() id {
	center := send(class("UNUserNotificationCenter"), "currentNotificationCenter")
	if b.notifyDelegate == 0 {
		b.notifyDelegate = alloc("MyGoNotificationDelegate")
		send(center, "setDelegate:", uintptr(b.notifyDelegate))
	}
	if !b.notifyAuthorized {
		b.notifyAuthorized = true
		blk := newBlock(func(_ objc.Block, granted bool, err id) {})
		send(center, "requestAuthorizationWithOptions:completionHandler:", 1<<0|1<<1|1<<2, uintptr(blk)) // badge | sound | alert
		blk.Release()
	}
	return center
}

func (b *Backend) ShowNotification(n *platform.Notification) error {
	if !b.NotificationsSupported() {
		return platform.ErrUnsupported
	}
	withPool(func() {
		center := b.notificationCenter()
		content := autorelease(alloc("UNMutableNotificationContent"))
		send(content, "setTitle:", uintptr(nsString(n.Title)))
		if n.Subtitle != "" {
			send(content, "setSubtitle:", uintptr(nsString(n.Subtitle)))
		}
		send(content, "setBody:", uintptr(nsString(n.Body)))
		if !n.Silent {
			send(content, "setSound:", uintptr(send(class("UNNotificationSound"), "defaultSound")))
		}
		req := send(class("UNNotificationRequest"), "requestWithIdentifier:content:trigger:",
			uintptr(nsString(n.ID)), uintptr(content), 0)
		send(center, "addNotificationRequest:withCompletionHandler:", uintptr(req), 0)
	})
	return nil
}

func (b *Backend) RemoveNotification(ident string) {
	if !b.NotificationsSupported() {
		return
	}
	withPool(func() {
		center := send(class("UNUserNotificationCenter"), "currentNotificationCenter")
		ids := nsArray(nsString(ident))
		send(center, "removeDeliveredNotificationsWithIdentifiers:", uintptr(ids))
		send(center, "removePendingNotificationRequestsWithIdentifiers:", uintptr(ids))
	})
}
