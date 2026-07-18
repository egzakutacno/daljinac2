package system

import (
	"runtime"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	openClipboard    = user32.NewProc("OpenClipboard")
	closeClipboard   = user32.NewProc("CloseClipboard")
	emptyClipboard   = user32.NewProc("EmptyClipboard")
	getClipboardData = user32.NewProc("GetClipboardData")
	setClipboardData = user32.NewProc("SetClipboardData")

	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	globalAlloc      = kernel32.NewProc("GlobalAlloc")
	globalLock       = kernel32.NewProc("GlobalLock")
	globalUnlock     = kernel32.NewProc("GlobalUnlock")
	globalFree       = kernel32.NewProc("GlobalFree")
)

const (
	CF_UNICODETEXT = 13
	GMEM_MOVEABLE  = 0x0002
)

func GetClipboard() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	openClipboard.Call(0)
	defer closeClipboard.Call()

	h, _, _ := getClipboardData.Call(CF_UNICODETEXT)
	if h == 0 {
		return ""
	}
	p, _, _ := globalLock.Call(h)
	if p == 0 {
		return ""
	}
	defer globalUnlock.Call(h)

	// Read UTF-16LE string from memory
	var result []uint16
	for i := 0; ; i++ {
		ch := *(*uint16)(unsafe.Pointer(p + uintptr(i*2)))
		if ch == 0 {
			break
		}
		result = append(result, ch)
	}
	return string(utf16.Decode(result))
}

func SetClipboard(text string) {
	if runtime.GOOS != "windows" {
		return
	}
	openClipboard.Call(0)
	emptyClipboard.Call(0)

	utf16Data := utf16.Encode([]rune(text + "\x00"))
	bytesLen := len(utf16Data) * 2

	h, _, _ := globalAlloc.Call(GMEM_MOVEABLE, uintptr(bytesLen))
	if h == 0 {
		closeClipboard.Call()
		return
	}
	p, _, _ := globalLock.Call(h)
	if p != 0 {
		for i, r := range utf16Data {
			*(*uint16)(unsafe.Pointer(p + uintptr(i*2))) = r
		}
		globalUnlock.Call(h)
	}
	setClipboardData.Call(CF_UNICODETEXT, h)
	closeClipboard.Call()
}
