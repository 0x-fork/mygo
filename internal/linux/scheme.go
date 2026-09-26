//go:build linux && (amd64 || arm64)

package linux

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"sync"
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/egoist/mygo/internal/platform"
)

var (
	cbScheme        ptr
	cbHeaderForeach ptr
	headerSinks     = map[ptr]http.Header{}
)

func init() {
	cbScheme = purego.NewCallback(func(req, data ptr) { theBackend.serveScheme(req) })
	cbHeaderForeach = purego.NewCallback(func(name, value, data ptr) {
		if h := headerSinks[data]; h != nil {
			h.Add(goStr(name), goStr(value))
		}
	})
}

func (b *Backend) registerScheme(scheme string) {
	if b.schemes[scheme] {
		return
	}
	b.schemes[scheme] = true
	ctx := webkitWebContextGetDefault()
	webkitWebContextRegisterURIScheme(ctx, cs(scheme), cbScheme, 0, 0)
	sec := webkitWebContextGetSecurityManager(ctx)
	webkitSecurityManagerRegisterSecure(sec, cs(scheme))
	webkitSecurityManagerRegisterCORS(sec, cs(scheme))
}

func (b *Backend) serveScheme(req ptr) {
	gObjectRef(req)
	ctx, cancel := context.WithCancel(context.Background())
	t := &schemeTask{req: req, cancel: cancel}
	w := b.byWebView[webkitURISchemeRequestGetWebView(req)]
	if w == nil || w.closed {
		t.Fail(context.Canceled)
		return
	}
	r := &platform.SchemeRequest{
		Context:   ctx,
		Method:    http.MethodGet,
		URL:       goStr(webkitURISchemeRequestGetURI(req)),
		Header:    http.Header{},
		Responder: t,
	}
	if webkitURISchemeRequestGetHTTPMethod != nil {
		if m := goStr(webkitURISchemeRequestGetHTTPMethod(req)); m != "" {
			r.Method = m
		}
	}
	if webkitURISchemeRequestGetHTTPHeaders != nil {
		if hdrs := webkitURISchemeRequestGetHTTPHeaders(req); hdrs != 0 {
			key := ptr(len(headerSinks) + 1)
			headerSinks[key] = r.Header
			soupMessageHeadersForeach(hdrs, cbHeaderForeach, key)
			delete(headerSinks, key)
		}
	}
	if webkitURISchemeRequestGetHTTPBody != nil {
		if stream := webkitURISchemeRequestGetHTTPBody(req); stream != 0 {
			r.Body = io.NopCloser(bytes.NewReader(readStream(stream)))
			gObjectUnref(stream)
		}
	}
	w.h.SchemeRequest(r)
}

func readStream(stream ptr) []byte {
	var out []byte
	buf := make([]byte, 64<<10)
	for {
		var n uintptr
		var gerr ptr
		ok := gInputStreamReadAll(stream, unsafe.Pointer(&buf[0]), uintptr(len(buf)), &n, 0, &gerr)
		out = append(out, buf[:n]...)
		if gerr != 0 {
			gErrorFree(gerr)
		}
		if !ok || n < uintptr(len(buf)) {
			return out
		}
	}
}

// schemeTask streams a response to WebKit through a pipe. Its methods run
// on the main thread; a goroutine feeds the pipe so the main loop, which is
// where WebKit reads it, never blocks.
type schemeTask struct {
	req       ptr
	cancel    context.CancelFunc
	responded bool
	done      bool

	mu     sync.Mutex
	cond   *sync.Cond
	queue  [][]byte
	closed bool
}

func (t *schemeTask) Respond(status int, header http.Header) {
	if t.done || t.responded {
		return
	}
	t.responded = true
	var fds [2]int
	if err := syscall.Pipe2(fds[:], syscall.O_CLOEXEC); err != nil {
		t.Fail(err)
		return
	}
	stream := gUnixInputStreamNew(int32(fds[0]), true)
	length := int64(-1)
	if cl, err := strconv.ParseInt(header.Get("Content-Length"), 10, 64); err == nil {
		length = cl
	}
	if webkitURISchemeRequestFinishWithResponse != nil {
		resp := webkitURISchemeResponseNew(stream, length)
		webkitURISchemeResponseSetStatus(resp, uint32(status), nil)
		if ct := header.Get("Content-Type"); ct != "" {
			webkitURISchemeResponseSetContentType(resp, cs(ct))
		}
		hdrs := soupMessageHeadersNew(1) // SOUP_MESSAGE_HEADERS_RESPONSE
		for k, vs := range header {
			for _, v := range vs {
				soupMessageHeadersAppend(hdrs, cs(k), cs(v))
			}
		}
		webkitURISchemeResponseSetHTTPHeaders(resp, hdrs)
		webkitURISchemeRequestFinishWithResponse(t.req, resp)
		gObjectUnref(resp)
	} else {
		// WebKitGTK before 2.36 cannot send status codes or headers.
		webkitURISchemeRequestFinish(t.req, stream, length, optCS(header.Get("Content-Type")))
	}
	gObjectUnref(stream)
	t.release()

	t.cond = sync.NewCond(&t.mu)
	go t.pump(fds[1])
}

func (t *schemeTask) pump(fd int) {
	defer syscall.Close(fd)
	for {
		t.mu.Lock()
		for len(t.queue) == 0 && !t.closed {
			t.cond.Wait()
		}
		if len(t.queue) == 0 {
			t.mu.Unlock()
			return
		}
		chunk := t.queue[0]
		t.queue[0] = nil
		t.queue = t.queue[1:]
		t.mu.Unlock()
		for len(chunk) > 0 {
			n, err := syscall.Write(fd, chunk)
			if err != nil {
				if err == syscall.EINTR {
					continue
				}
				// The page no longer wants the response.
				t.cancel()
				t.mu.Lock()
				t.queue, t.closed = nil, true
				t.mu.Unlock()
				return
			}
			chunk = chunk[n:]
		}
	}
}

func (t *schemeTask) Write(p []byte) {
	if t.done {
		return
	}
	if !t.responded {
		t.Respond(http.StatusOK, http.Header{})
	}
	if t.cond == nil {
		return
	}
	t.mu.Lock()
	if !t.closed {
		t.queue = append(t.queue, p)
	}
	t.mu.Unlock()
	t.cond.Signal()
}

func (t *schemeTask) Finish() {
	if t.done {
		return
	}
	if !t.responded {
		t.Respond(http.StatusOK, http.Header{})
	}
	t.done = true
	if t.cond != nil {
		t.mu.Lock()
		t.closed = true
		t.mu.Unlock()
		t.cond.Signal()
	}
}

func (t *schemeTask) Fail(err error) {
	if t.done {
		return
	}
	if !t.responded {
		t.responded = true
		gerr := gErrorNewLiteral(gQuarkFromString(cs("mygo")), 1, cs(err.Error()))
		webkitURISchemeRequestFinishError(t.req, gerr)
		gErrorFree(gerr)
		t.release()
	}
	t.Finish()
}

func (t *schemeTask) release() {
	if t.req != 0 {
		gObjectUnref(t.req)
		t.req = 0
	}
}
