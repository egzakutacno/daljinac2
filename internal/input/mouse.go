package input

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32           = windows.NewLazySystemDLL("user32.dll")
	procSetCursorPos = user32.NewProc("SetCursorPos")
	procSendInput    = user32.NewProc("SendInput")
	procGetDpiForWindow = user32.NewProc("GetDpiForWindow")
)

const (
	INPUT_MOUSE      = 0
	MOUSEEVENTF_MOVE        = 0x0001
	MOUSEEVENTF_LEFTDOWN    = 0x0002
	MOUSEEVENTF_LEFTUP      = 0x0004
	MOUSEEVENTF_RIGHTDOWN   = 0x0008
	MOUSEEVENTF_RIGHTUP     = 0x0010
	MOUSEEVENTF_MIDDLEDOWN  = 0x0020
	MOUSEEVENTF_MIDDLEUP    = 0x0040
	MOUSEEVENTF_WHEEL       = 0x0800
	MOUSEEVENTF_ABSOLUTE    = 0x8000
	MOUSEEVENTF_VIRTUALDESK = 0x4000
	WHEEL_DELTA   = 120
)

type MOUSEINPUT struct {
	Dx          int32
	Dy          int32
	MouseData   uint32
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

type INPUT struct {
	Type uint32
	Mi   MOUSEINPUT
}

func Move(x, y int) {
	procSetCursorPos.Call(uintptr(x), uintptr(y))
}

func Click(x, y int, button string) {
	Move(x, y)
	var downFlag, upFlag uint32
	switch button {
	case "right":
		downFlag = MOUSEEVENTF_RIGHTDOWN
		upFlag = MOUSEEVENTF_RIGHTUP
	case "middle":
		downFlag = MOUSEEVENTF_MIDDLEDOWN
		upFlag = MOUSEEVENTF_MIDDLEUP
	default:
		downFlag = MOUSEEVENTF_LEFTDOWN
		upFlag = MOUSEEVENTF_LEFTUP
	}
	sendMouseInput(downFlag)
	sendMouseInput(upFlag)
}

func DoubleClick(x, y int, button string) {
	Click(x, y, button)
	Click(x, y, button)
}

func ClickCurrent(button string) {
	var downFlag, upFlag uint32
	switch button {
	case "right":
		downFlag = MOUSEEVENTF_RIGHTDOWN
		upFlag = MOUSEEVENTF_RIGHTUP
	case "middle":
		downFlag = MOUSEEVENTF_MIDDLEDOWN
		upFlag = MOUSEEVENTF_MIDDLEUP
	default:
		downFlag = MOUSEEVENTF_LEFTDOWN
		upFlag = MOUSEEVENTF_LEFTUP
	}
	sendMouseInput(downFlag)
	sendMouseInput(upFlag)
}

func Scroll(x, y int, deltaX, deltaY int) {
	Move(x, y)
	if deltaY != 0 {
		data := uint32(uint16(uint32(-deltaY * WHEEL_DELTA)))
		inp := INPUT{
			Type: INPUT_MOUSE,
			Mi: MOUSEINPUT{
				DwFlags:   MOUSEEVENTF_WHEEL,
				MouseData: data,
			},
		}
		procSendInput.Call(1, uintptr(unsafe.Pointer(&inp)), unsafe.Sizeof(inp))
	}
	if deltaX != 0 {
		// For horizontal scroll, use WM_MOUSEHWHEEL via MOUSEEVENTF_HWHEEL
		hwheelFlag := uint32(0x0100)
		data := uint32(uint16(uint32(-deltaX * WHEEL_DELTA)))
		inp := INPUT{
			Type: INPUT_MOUSE,
			Mi: MOUSEINPUT{
				DwFlags:   hwheelFlag,
				MouseData: data,
			},
		}
		procSendInput.Call(1, uintptr(unsafe.Pointer(&inp)), unsafe.Sizeof(inp))
	}
}

func Drag(fromX, fromY, toX, toY int) {
	Move(fromX, fromY)
	sendMouseInput(MOUSEEVENTF_LEFTDOWN)
	Move(toX, toY)
	sendMouseInput(MOUSEEVENTF_LEFTUP)
}

func sendMouseInput(flag uint32) {
	inp := INPUT{
		Type: INPUT_MOUSE,
		Mi: MOUSEINPUT{
			DwFlags: flag,
		},
	}
	procSendInput.Call(1, uintptr(unsafe.Pointer(&inp)), unsafe.Sizeof(inp))
}
