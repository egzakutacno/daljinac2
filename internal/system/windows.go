package system

import (
	"fmt"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var (
	enumWindows      = user32.NewProc("EnumWindows")
	getWindowTextW   = user32.NewProc("GetWindowTextW")
	getWindowTextLenW = user32.NewProc("GetWindowTextLengthW")
	getClassNameW    = user32.NewProc("GetClassNameW")
	isWindowVisible  = user32.NewProc("IsWindowVisible")
	getWindowRect    = user32.NewProc("GetWindowRect")
	getWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
)

type WindowInfo struct {
	HWND      uintptr `json:"-"`
	Title     string  `json:"title"`
	ClassName string  `json:"class_name"`
	Pid       uint32  `json:"pid"`
	Visible   bool    `json:"visible"`
	X         int32   `json:"x"`
	Y         int32   `json:"y"`
	Width     int32   `json:"width"`
	Height    int32   `json:"height"`
}

type RECT struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

func Windows() ([]WindowInfo, error) {
	var result []WindowInfo
	cb := syscall.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
		visible, _, _ := isWindowVisible.Call(hwnd)
		title := getWindowText(hwnd)
		class := getClassName(hwnd)

		var pid uint32
		getWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))

		var rect RECT
		getWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))

		info := WindowInfo{
			HWND:      hwnd,
			Title:     title,
			ClassName: class,
			Pid:       pid,
			Visible:   visible != 0,
			X:         rect.Left,
			Y:         rect.Top,
			Width:     rect.Right - rect.Left,
			Height:    rect.Bottom - rect.Top,
		}
		result = append(result, info)
		return 1
	})

	enumWindows.Call(cb, 0)
	if len(result) == 0 {
		return nil, fmt.Errorf("no windows found")
	}
	return result, nil
}

func getWindowText(hwnd uintptr) string {
	len, _, _ := getWindowTextLenW.Call(hwnd)
	if len == 0 {
		return ""
	}
	buf := make([]uint16, len+1)
	getWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), len+1)
	return string(utf16.Decode(buf[:len]))
}

func getClassName(hwnd uintptr) string {
	buf := make([]uint16, 256)
	getClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), 256)
	return string(utf16.Decode(buf[:]))
}
