package input

import (
	"unicode"
	"unsafe"
)

const (
	INPUT_KEYBOARD = 1
	KEYEVENTF_KEYDOWN = 0x0000
	KEYEVENTF_KEYUP   = 0x0002
	KEYEVENTF_SCANCODE = 0x0008
	KEYEVENTF_UNICODE  = 0x0004
)

type KEYBDINPUT struct {
	WVk         uint16
	WScan       uint16
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

type INPUT_KEY struct {
	Type uint32
	Ki   KEYBDINPUT
}

var vkMap = map[string]uint16{
	"backspace": 0x08, "tab": 0x09, "enter": 0x0D, "shift": 0x10,
	"ctrl": 0x11, "alt": 0x12, "pause": 0x13, "capslock": 0x14,
	"escape": 0x1B, "space": 0x20, "pageup": 0x21, "pagedown": 0x22,
	"end": 0x23, "home": 0x24, "left": 0x25, "up": 0x26,
	"right": 0x27, "down": 0x28, "printscreen": 0x2C, "insert": 0x2D,
	"delete": 0x2E, "0": 0x30, "1": 0x31, "2": 0x32,
	"3": 0x33, "4": 0x34, "5": 0x35, "6": 0x36,
	"7": 0x37, "8": 0x38, "9": 0x39, "a": 0x41,
	"b": 0x42, "c": 0x43, "d": 0x44, "e": 0x45,
	"f": 0x46, "g": 0x47, "h": 0x48, "i": 0x49,
	"j": 0x4A, "k": 0x4B, "l": 0x4C, "m": 0x4D,
	"n": 0x4E, "o": 0x4F, "p": 0x50, "q": 0x51,
	"r": 0x52, "s": 0x53, "t": 0x54, "u": 0x55,
	"v": 0x56, "w": 0x57, "x": 0x58, "y": 0x59,
	"z": 0x5A, "win": 0x5B, "menu": 0x5D, "numpad0": 0x60,
	"numpad1": 0x61, "numpad2": 0x62, "numpad3": 0x63,
	"numpad4": 0x64, "numpad5": 0x65, "numpad6": 0x66,
	"numpad7": 0x67, "numpad8": 0x68, "numpad9": 0x69,
	"multiply": 0x6A, "add": 0x6B, "subtract": 0x6D, "decimal": 0x6E,
	"divide": 0x6F, "f1": 0x70, "f2": 0x71, "f3": 0x72,
	"f4": 0x73, "f5": 0x74, "f6": 0x75, "f7": 0x76,
	"f8": 0x77, "f9": 0x78, "f10": 0x79, "f11": 0x7A,
	"f12": 0x7B, "numlock": 0x90, "scrolllock": 0x91,
	"shift_r": 0xA0, "ctrl_r": 0xA1, "alt_r": 0xA2,
	";": 0xBA, "=": 0xBB, ",": 0xBC, "-": 0xBD,
	".": 0xBE, "/": 0xBF, "`": 0xC0,
}

func keyDown(vk uint16) {
	inp := INPUT_KEY{Type: INPUT_KEYBOARD, Ki: KEYBDINPUT{WVk: vk, DwFlags: KEYEVENTF_KEYDOWN}}
	procSendInput.Call(1, uintptr(unsafe.Pointer(&inp)), unsafe.Sizeof(inp))
}

func keyUp(vk uint16) {
	inp := INPUT_KEY{Type: INPUT_KEYBOARD, Ki: KEYBDINPUT{WVk: vk, DwFlags: KEYEVENTF_KEYUP}}
	procSendInput.Call(1, uintptr(unsafe.Pointer(&inp)), unsafe.Sizeof(inp))
}

func TypeText(text string) {
	for _, r := range text {
		if r == '\n' {
			keyDown(0x0D)
			keyUp(0x0D)
			continue
		}
		// Use Unicode input for safety
		inp := INPUT_KEY{
			Type: INPUT_KEYBOARD,
			Ki: KEYBDINPUT{
				WVk:       0,
				WScan:     uint16(r),
				DwFlags:   KEYEVENTF_UNICODE | KEYEVENTF_KEYDOWN,
			},
		}
		procSendInput.Call(1, uintptr(unsafe.Pointer(&inp)), unsafe.Sizeof(inp))
		inp.Ki.DwFlags = KEYEVENTF_UNICODE | KEYEVENTF_KEYUP
		procSendInput.Call(1, uintptr(unsafe.Pointer(&inp)), unsafe.Sizeof(inp))
	}
}

func Hotkey(keys []string) {
	vks := make([]uint16, 0, len(keys))
	for _, k := range keys {
		k = lowerKey(k)
		if v, ok := vkMap[k]; ok {
			vks = append(vks, v)
		}
	}
	for _, vk := range vks {
		keyDown(vk)
	}
	for i := len(vks) - 1; i >= 0; i-- {
		keyUp(vks[i])
	}
}

func KeyPress(key string) {
	k := lowerKey(key)
	if v, ok := vkMap[k]; ok {
		keyDown(v)
		keyUp(v)
	}
}

func lowerKey(k string) string {
	if len(k) == 1 && k[0] >= 'A' && k[0] <= 'Z' {
		return string(unicode.ToLower(rune(k[0])))
	}
	return k
}
