//go:build windows && (amd64 || arm64)

// Package windows implements the MyGo backend for Windows: Win32 windows
// hosting WebView2, called through the syscall package (no cgo).
package windows

import (
	"syscall"
	"unicode/utf16"
	"unsafe"
)

// System DLLs are loaded from the system directory, never from the
// application directory or the search path.
var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll") // a KnownDLL
	sysDir   = systemDirectory()

	user32   = systemDLL("user32.dll")
	gdi32    = systemDLL("gdi32.dll")
	ole32    = systemDLL("ole32.dll")
	shell32  = systemDLL("shell32.dll")
	shlwapi  = systemDLL("shlwapi.dll")
	dwmapi   = systemDLL("dwmapi.dll")
	shcore   = systemDLL("shcore.dll")
	advapi32 = systemDLL("advapi32.dll")
	versionD = systemDLL("version.dll")
)

func systemDirectory() string {
	buf := make([]uint16, syscall.MAX_PATH)
	n, _, _ := kernel32.NewProc("GetSystemDirectoryW").Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 || int(n) > len(buf) {
		return `C:\Windows\System32`
	}
	return syscall.UTF16ToString(buf[:n])
}

func systemDLL(name string) *syscall.LazyDLL { return syscall.NewLazyDLL(sysDir + `\` + name) }

// has reports whether an optional API exists on this version of Windows.
func has(p *syscall.LazyProc) bool { return p.Find() == nil }

var (
	// kernel32
	procGetModuleHandleW       = kernel32.NewProc("GetModuleHandleW")
	procGetCurrentThreadId     = kernel32.NewProc("GetCurrentThreadId")
	procGlobalAlloc            = kernel32.NewProc("GlobalAlloc")
	procGlobalFree             = kernel32.NewProc("GlobalFree")
	procGlobalLock             = kernel32.NewProc("GlobalLock")
	procGlobalUnlock           = kernel32.NewProc("GlobalUnlock")
	procGlobalSize             = kernel32.NewProc("GlobalSize")
	procGetUserDefaultLocaleNm = kernel32.NewProc("GetUserDefaultLocaleName")
	procCreateActCtxW          = kernel32.NewProc("CreateActCtxW")
	procActivateActCtx         = kernel32.NewProc("ActivateActCtx")
	procLoadLibraryExW         = kernel32.NewProc("LoadLibraryExW")
	procGetProcAddress         = kernel32.NewProc("GetProcAddress")

	// user32
	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	procGetDpiForWindow               = user32.NewProc("GetDpiForWindow")
	procGetDpiForSystem               = user32.NewProc("GetDpiForSystem")
	procAdjustWindowRectExForDpi      = user32.NewProc("AdjustWindowRectExForDpi")
	procGetSystemMetricsForDpi        = user32.NewProc("GetSystemMetricsForDpi")
	procRegisterClassExW              = user32.NewProc("RegisterClassExW")
	procCreateWindowExW               = user32.NewProc("CreateWindowExW")
	procDestroyWindow                 = user32.NewProc("DestroyWindow")
	procDefWindowProcW                = user32.NewProc("DefWindowProcW")
	procGetMessageW                   = user32.NewProc("GetMessageW")
	procPeekMessageW                  = user32.NewProc("PeekMessageW")
	procTranslateMessage              = user32.NewProc("TranslateMessage")
	procDispatchMessageW              = user32.NewProc("DispatchMessageW")
	procPostMessageW                  = user32.NewProc("PostMessageW")
	procSendMessageW                  = user32.NewProc("SendMessageW")
	procPostQuitMessage               = user32.NewProc("PostQuitMessage")
	procMsgWaitForMultipleObjectsEx   = user32.NewProc("MsgWaitForMultipleObjectsEx")
	procShowWindow                    = user32.NewProc("ShowWindow")
	procIsWindowVisible               = user32.NewProc("IsWindowVisible")
	procIsIconic                      = user32.NewProc("IsIconic")
	procIsZoomed                      = user32.NewProc("IsZoomed")
	procSetWindowPos                  = user32.NewProc("SetWindowPos")
	procGetWindowRect                 = user32.NewProc("GetWindowRect")
	procGetClientRect                 = user32.NewProc("GetClientRect")
	procClientToScreen                = user32.NewProc("ClientToScreen")
	procSetWindowTextW                = user32.NewProc("SetWindowTextW")
	procGetWindowTextW                = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW          = user32.NewProc("GetWindowTextLengthW")
	procGetWindowLongPtrW             = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW             = user32.NewProc("SetWindowLongPtrW")
	procSetForegroundWindow           = user32.NewProc("SetForegroundWindow")
	procGetForegroundWindow           = user32.NewProc("GetForegroundWindow")
	procSetFocus                      = user32.NewProc("SetFocus")
	procEnableWindow                  = user32.NewProc("EnableWindow")
	procGetWindowPlacement            = user32.NewProc("GetWindowPlacement")
	procSetWindowPlacement            = user32.NewProc("SetWindowPlacement")
	procMonitorFromWindow             = user32.NewProc("MonitorFromWindow")
	procMonitorFromPoint              = user32.NewProc("MonitorFromPoint")
	procGetMonitorInfoW               = user32.NewProc("GetMonitorInfoW")
	procEnumDisplayMonitors           = user32.NewProc("EnumDisplayMonitors")
	procGetCursorPos                  = user32.NewProc("GetCursorPos")
	procLoadCursorW                   = user32.NewProc("LoadCursorW")
	procLoadIconW                     = user32.NewProc("LoadIconW")
	procCreateIconFromResourceEx      = user32.NewProc("CreateIconFromResourceEx")
	procDestroyIcon                   = user32.NewProc("DestroyIcon")
	procReleaseCapture                = user32.NewProc("ReleaseCapture")
	procGetSystemMenu                 = user32.NewProc("GetSystemMenu")
	procEnableMenuItem                = user32.NewProc("EnableMenuItem")
	procCreateMenu                    = user32.NewProc("CreateMenu")
	procCreatePopupMenu               = user32.NewProc("CreatePopupMenu")
	procDestroyMenu                   = user32.NewProc("DestroyMenu")
	procAppendMenuW                   = user32.NewProc("AppendMenuW")
	procSetMenu                       = user32.NewProc("SetMenu")
	procDrawMenuBar                   = user32.NewProc("DrawMenuBar")
	procSetMenuItemInfoW              = user32.NewProc("SetMenuItemInfoW")
	procTrackPopupMenuEx              = user32.NewProc("TrackPopupMenuEx")
	procGetKeyState                   = user32.NewProc("GetKeyState")
	procRegisterHotKey                = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey              = user32.NewProc("UnregisterHotKey")
	procMessageBeep                   = user32.NewProc("MessageBeep")
	procMessageBoxW                   = user32.NewProc("MessageBoxW")
	procFlashWindowEx                 = user32.NewProc("FlashWindowEx")
	procSetLayeredWindowAttributes    = user32.NewProc("SetLayeredWindowAttributes")
	procSetWindowDisplayAffinity      = user32.NewProc("SetWindowDisplayAffinity")
	procRegisterWindowMessageW        = user32.NewProc("RegisterWindowMessageW")
	procOpenClipboard                 = user32.NewProc("OpenClipboard")
	procCloseClipboard                = user32.NewProc("CloseClipboard")
	procEmptyClipboard                = user32.NewProc("EmptyClipboard")
	procGetClipboardData              = user32.NewProc("GetClipboardData")
	procSetClipboardData              = user32.NewProc("SetClipboardData")
	procIsClipboardFormatAvailable    = user32.NewProc("IsClipboardFormatAvailable")
	procRegisterClipboardFormatW      = user32.NewProc("RegisterClipboardFormatW")
	procEnumClipboardFormats          = user32.NewProc("EnumClipboardFormats")
	procGetClipboardFormatNameW       = user32.NewProc("GetClipboardFormatNameW")
	procFillRect                      = user32.NewProc("FillRect")
	procGetNextWindow                 = user32.NewProc("GetWindow")
	procInvalidateRectW               = user32.NewProc("InvalidateRect")

	// gdi32
	procCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	procDeleteObject     = gdi32.NewProc("DeleteObject")

	// ole32
	procCoInitializeEx        = ole32.NewProc("CoInitializeEx")
	procCoCreateInstance      = ole32.NewProc("CoCreateInstance")
	procCoTaskMemAlloc        = ole32.NewProc("CoTaskMemAlloc")
	procCoTaskMemFree         = ole32.NewProc("CoTaskMemFree")
	procCreateStreamOnHGlobal = ole32.NewProc("CreateStreamOnHGlobal")
	procGetHGlobalFromStream  = ole32.NewProc("GetHGlobalFromStream")

	// shell32
	procShellExecuteW               = shell32.NewProc("ShellExecuteW")
	procSHFileOperationW            = shell32.NewProc("SHFileOperationW")
	procShellNotifyIconW            = shell32.NewProc("Shell_NotifyIconW")
	procShellNotifyIconGetRect      = shell32.NewProc("Shell_NotifyIconGetRect")
	procSHCreateItemFromParsingName = shell32.NewProc("SHCreateItemFromParsingName")

	// shlwapi
	procSHCreateMemStream = shlwapi.NewProc("SHCreateMemStream")

	// dwmapi
	procDwmSetWindowAttribute        = dwmapi.NewProc("DwmSetWindowAttribute")
	procDwmExtendFrameIntoClientArea = dwmapi.NewProc("DwmExtendFrameIntoClientArea")

	// shcore
	procGetDpiForMonitor = shcore.NewProc("GetDpiForMonitor")

	// advapi32
	procRegOpenKeyExW    = advapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueExW = advapi32.NewProc("RegQueryValueExW")
	procRegCloseKey      = advapi32.NewProc("RegCloseKey")

	// version
	procGetFileVersionInfoSizeW = versionD.NewProc("GetFileVersionInfoSizeW")
	procGetFileVersionInfoW     = versionD.NewProc("GetFileVersionInfoW")
	procVerQueryValueW          = versionD.NewProc("VerQueryValueW")
)

// Window messages and constants.
const (
	wmDestroy           = 0x0002
	wmMove              = 0x0003
	wmSize              = 0x0005
	wmActivate          = 0x0006
	wmSetFocus          = 0x0007
	wmClose             = 0x0010
	wmQueryEndSession   = 0x0011
	wmQuit              = 0x0012
	wmEraseBkgnd        = 0x0014
	wmEndSession        = 0x0016
	wmSettingChange     = 0x001A
	wmGetMinMaxInfo     = 0x0024
	wmWindowPosChanging = 0x0046
	wmSetIcon           = 0x0080
	wmNCCalcSize        = 0x0083
	wmNCHitTest         = 0x0084
	wmNCLButtonDown     = 0x00A1
	wmCommand           = 0x0111
	wmSysCommand        = 0x0112
	wmLButtonUp         = 0x0202
	wmRButtonUp         = 0x0205
	wmContextMenu       = 0x007B
	wmHotkey            = 0x0312
	wmDisplayChange     = 0x007E
	wmDpiChanged        = 0x02E0
	wmUser              = 0x0400
	wmApp               = 0x8000

	wmAppDispatch = wmApp + 1
	wmAppWake     = wmApp + 2
	wmAppTray     = wmApp + 3
	wmAppReady    = wmApp + 4

	wsOverlapped       = 0x00000000
	wsPopup            = 0x80000000
	wsVisible          = 0x10000000
	wsClipChildren     = 0x02000000
	wsCaption          = 0x00C00000
	wsSysMenu          = 0x00080000
	wsThickFrame       = 0x00040000
	wsMinimizeBox      = 0x00020000
	wsMaximizeBox      = 0x00010000
	wsOverlappedWindow = wsOverlapped | wsCaption | wsSysMenu | wsThickFrame | wsMinimizeBox | wsMaximizeBox

	wsExTopmost     = 0x00000008
	wsExTransparent = 0x00000020
	wsExToolWindow  = 0x00000080
	wsExAppWindow   = 0x00040000
	wsExLayered     = 0x00080000
	wsExNoActivate  = 0x08000000

	gwlStyle   = -16
	gwlExStyle = -20

	swHide           = 0
	swShowNormal     = 1
	swShowMinimized  = 2
	swShowMaximized  = 3
	swShowNoActivate = 4
	swShow           = 5
	swMinimize       = 6
	swRestore        = 9

	swpNoSize       = 0x0001
	swpNoMove       = 0x0002
	swpNoZOrder     = 0x0004
	swpNoActivate   = 0x0010
	swpFrameChanged = 0x0020

	hwndTopmost   = ^uintptr(0)     // -1
	hwndNoTopmost = ^uintptr(0) - 1 // -2
	hwndMessage   = ^uintptr(0) - 2 // -3

	sizeRestored  = 0
	sizeMinimized = 1
	sizeMaximized = 2

	waInactive = 0

	htClient      = 1
	htCaption     = 2
	htLeft        = 10
	htRight       = 11
	htTop         = 12
	htTopLeft     = 13
	htTopRight    = 14
	htBottom      = 15
	htBottomLeft  = 16
	htBottomRight = 17

	scClose = 0xF060

	mfString    = 0x0000
	mfGrayed    = 0x0001
	mfChecked   = 0x0008
	mfPopup     = 0x0010
	mfSeparator = 0x0800
	mfByCommand = 0x0000

	miimState     = 0x0001
	miimFType     = 0x0100
	miimString    = 0x0040
	mftRadioCheck = 0x0200
	mfsChecked    = 0x0008
	mfsDisabled   = 0x0003

	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100

	monitorDefaultToNearest = 2
	monitorDefaultToPrimary = 1
	monitorInfoPrimary      = 1

	smCxFrame        = 32
	smCyFrame        = 33
	smCxPaddedBorder = 92

	vkShift   = 0x10
	vkControl = 0x11
	vkMenu    = 0x12
	vkLWin    = 0x5B
	vkRWin    = 0x5C

	cfUnicodeText = 13
	cfDIB         = 8
	cfDIBV5       = 17
	gmemMoveable  = 0x0002

	idcArrow = 32512

	dwmwaUseImmersiveDarkMode = 20
	dwmwaSystemBackdropType   = 38

	lwaAlpha = 0x2

	wdaNone               = 0
	wdaExcludeFromCapture = 0x11

	coinitApartmentThreaded = 0x2
	clsctxInprocServer      = 0x1

	dpiAwarenessContextPerMonitorAwareV2 = ^uintptr(0) - 3 // -4

	qsAllInput         = 0x04FF
	mwmoInputAvailable = 0x0004
	pmRemove           = 0x0001

	sOK          = 0
	eNoInterface = 0x80004002
)

type rect struct{ Left, Top, Right, Bottom int32 }

type point struct{ X, Y int32 }

type msg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	Private uint32
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type minMaxInfo struct {
	Reserved, MaxSize, MaxPosition, MinTrackSize, MaxTrackSize point
}

type windowPlacement struct {
	Length         uint32
	Flags          uint32
	ShowCmd        uint32
	MinPosition    point
	MaxPosition    point
	NormalPosition rect
}

type monitorInfoEx struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
	Device  [32]uint16
}

type windowPos struct {
	HWnd, InsertAfter uintptr
	X, Y, CX, CY      int32
	Flags             uint32
}

type ncCalcSizeParams struct {
	Rgrc [3]rect
	Pos  *windowPos
}

type margins struct{ Left, Right, Top, Bottom int32 }

type flashWInfo struct {
	Size    uint32
	HWnd    uintptr
	Flags   uint32
	Count   uint32
	Timeout uint32
}

// GUID is a COM interface or class identifier.
type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// guid parses "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx".
func guid(s string) GUID {
	hex := func(s string) uint64 {
		var v uint64
		for _, c := range s {
			v <<= 4
			switch {
			case c >= '0' && c <= '9':
				v |= uint64(c - '0')
			case c >= 'a' && c <= 'f':
				v |= uint64(c-'a') + 10
			case c >= 'A' && c <= 'F':
				v |= uint64(c-'A') + 10
			}
		}
		return v
	}
	g := GUID{Data1: uint32(hex(s[0:8])), Data2: uint16(hex(s[9:13])), Data3: uint16(hex(s[14:18]))}
	g.Data4[0], g.Data4[1] = byte(hex(s[19:21])), byte(hex(s[21:23]))
	for i := 0; i < 6; i++ {
		g.Data4[2+i] = byte(hex(s[24+2*i : 26+2*i]))
	}
	return g
}

// u16 converts s to a NUL-terminated UTF-16 string; NULs inside s are
// dropped. The result must stay reachable during the call it is passed
// to: pass it as uintptr(unsafe.Pointer(u16(s))) directly in the call.
func u16(s string) *uint16 { return &utf16z(s)[0] }

// utf16z is u16 as a slice, NUL included.
func utf16z(s string) []uint16 {
	b := make([]uint16, 0, len(s)+1)
	for _, r := range s {
		if r == 0 {
			continue
		}
		if r >= 0x10000 {
			r1, r2 := utf16.EncodeRune(r)
			b = append(b, uint16(r1), uint16(r2))
		} else {
			b = append(b, uint16(r))
		}
	}
	return append(b, 0)
}

// utf16Units is s in UTF-16, without a terminating NUL.
func utf16Units(s string) []uint16 {
	b := utf16z(s)
	return b[:len(b)-1]
}

// u16Opt is u16, or nil for an empty string.
func u16Opt(s string) *uint16 {
	if s == "" {
		return nil
	}
	return u16(s)
}

// native turns an address of memory not managed by Go into a pointer.
func native(addr uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&addr)) }

// wstr reads a NUL-terminated UTF-16 string at a native address.
func wstr(p uintptr) string {
	if p == 0 {
		return ""
	}
	n := 0
	for *(*uint16)(unsafe.Add(native(p), 2*n)) != 0 {
		n++
	}
	return string(utf16.Decode(unsafe.Slice((*uint16)(native(p)), n)))
}

// takeWstr reads a string allocated with CoTaskMemAlloc and frees it.
func takeWstr(p uintptr) string {
	s := wstr(p)
	if p != 0 {
		procCoTaskMemFree.Call(p)
	}
	return s
}

func loword(v uintptr) uint16 { return uint16(v) }
func hiword(v uintptr) uint16 { return uint16(v >> 16) }

// getXParam and getYParam decode the signed coordinates of an lParam.
func getXParam(v uintptr) int32 { return int32(int16(loword(v))) }
func getYParam(v uintptr) int32 { return int32(int16(hiword(v))) }

func currentThreadID() uint32 {
	id, _, _ := procGetCurrentThreadId.Call()
	return uint32(id)
}

func instance() uintptr {
	h, _, _ := procGetModuleHandleW.Call(0)
	return h
}

func windowLong(hwnd uintptr, index int32) uintptr {
	r, _, _ := procGetWindowLongPtrW.Call(hwnd, uintptr(index))
	return r
}

func setWindowLong(hwnd uintptr, index int32, v uintptr) {
	procSetWindowLongPtrW.Call(hwnd, uintptr(index), v)
}

func postMessage(hwnd uintptr, m uint32, w, l uintptr) {
	procPostMessageW.Call(hwnd, uintptr(m), w, l)
}

func dpiOf(hwnd uintptr) int {
	if hwnd != 0 && has(procGetDpiForWindow) {
		if d, _, _ := procGetDpiForWindow.Call(hwnd); d != 0 {
			return int(d)
		}
	}
	if has(procGetDpiForSystem) {
		if d, _, _ := procGetDpiForSystem.Call(); d != 0 {
			return int(d)
		}
	}
	return 96
}

// Conversions between DIPs (the unit of the public API) and pixels.
func toPx(v, dpi int) int32      { return int32((v*dpi + 48) / 96) }
func toDIP(v int32, dpi int) int { return (int(v)*96 + dpi/2) / dpi }

// commonControl resolves a function of the common controls library. It is
// loaded by name, so that the activation context (see
// activateCommonControls) selects version 6, and only from the system
// directory.
func commonControl(name string) uintptr {
	const loadLibrarySearchSystem32 = 0x800
	h, _, _ := procLoadLibraryExW.Call(uintptr(unsafe.Pointer(u16("comctl32.dll"))), 0, loadLibrarySearchSystem32)
	if h == 0 {
		return 0
	}
	cname := append([]byte(name), 0)
	p, _, _ := procGetProcAddress.Call(h, uintptr(unsafe.Pointer(&cname[0])))
	return p
}
