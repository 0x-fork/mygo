package ui

import (
	"weak"

	"github.com/egoist/mygo/transfer"
)

// Services provides persistent window services for custom controls and
// asynchronous work. It owns no frame data and does not retain the window.
// Clipboard and URL methods run on the UI thread; Invalidate may run on
// any goroutine. Models are still updated through Window.Update.
type Services struct{ owner weak.Pointer[engine] }

// Services returns the persistent services of this frame's window.
func (c *Context) Services() Services {
	if c == nil {
		return Services{}
	}
	return c.services
}

func (s Services) Invalidate() {
	if rt := s.owner.Value(); rt != nil {
		rt.host.invalidate()
	}
}
func (s Services) ReadClipboard() string {
	if rt := s.owner.Value(); rt != nil && !rt.closed {
		return rt.host.readClipboard()
	}
	return ""
}

// ReadClipboardImage reads the clipboard's image as PNG, nil without one.
func (s Services) ReadClipboardImage() []byte {
	if rt := s.owner.Value(); rt != nil && !rt.closed {
		return rt.host.readClipboardImage()
	}
	return nil
}
func (s Services) WriteClipboard(text string) {
	if rt := s.owner.Value(); rt != nil && !rt.closed {
		rt.host.writeClipboard(text)
	}
}

// ReadClipboardData reads the clipboard's representations of formats, as
// a rich text editor reads its own format, HTML and plain text to paste
// the richest it understands; nil formats read all. A clipboard without
// them gives an empty Data.
func (s Services) ReadClipboardData(formats ...transfer.Format) transfer.Data {
	if rt := s.owner.Value(); rt != nil && !rt.closed {
		return rt.host.readClipboardData(formats)
	}
	return transfer.Data{}
}

// WriteClipboardData puts data on the clipboard, each representation of
// its items in the system's formats, as a rich text editor copies its own
// format, HTML and plain text.
func (s Services) WriteClipboardData(data transfer.Data) {
	if rt := s.owner.Value(); rt != nil && !rt.closed {
		rt.host.writeClipboardData(data)
	}
}

func (s Services) OpenURL(url string) {
	if rt := s.owner.Value(); rt != nil && !rt.closed {
		rt.host.openURL(url, nil)
	}
}
