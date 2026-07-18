package tray

import (
	"log"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

var (
	user32 = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32 = syscall.NewLazyDLL("shell32.dll")

	getModuleHandleW      = kernel32.NewProc("GetModuleHandleW")
	getLastError          = kernel32.NewProc("GetLastError")
	registerClassExW      = user32.NewProc("RegisterClassExW")
	createWindowExW       = user32.NewProc("CreateWindowExW")
	defWindowProcW        = user32.NewProc("DefWindowProcW")
	destroyWindow         = user32.NewProc("DestroyWindow")
	postQuitMessage       = user32.NewProc("PostQuitMessage")
	getMessageW           = user32.NewProc("GetMessageW")
	translateMessage      = user32.NewProc("TranslateMessage")
	dispatchMessageW      = user32.NewProc("DispatchMessageW")
	shellNotifyIconW      = shell32.NewProc("Shell_NotifyIconW")
	loadIconW             = user32.NewProc("LoadIconW")
	loadCursorW           = user32.NewProc("LoadCursorW")
	createPopupMenu       = user32.NewProc("CreatePopupMenu")
	appendMenuW           = user32.NewProc("AppendMenuW")
	trackPopupMenu        = user32.NewProc("TrackPopupMenu")
	setForegroundWindow   = user32.NewProc("SetForegroundWindow")
	postMessageW          = user32.NewProc("PostMessageW")
	destroyMenu           = user32.NewProc("DestroyMenu")
)

const (
	WM_DESTROY          = 2
	WM_COMMAND          = 0x0111
	WM_APP              = 0x8000
	NIM_ADD             = 0
	NIM_DELETE          = 2
	NIM_SETVERSION      = 4
	NIF_MESSAGE         = 1
	NIF_ICON            = 2
	NIF_TIP             = 4
	NOTIFYICON_VERSION_4 = 4
	MF_STRING     = 0
	MF_SEPARATOR  = 0x0800
	IDI_APPLICATION = 32512
	IDC_ARROW       = 32512
)

const (
	IconConnecting = 0
	IconConnected  = 1
	IconError      = 2
)

type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type NOTIFYICONDATAW struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

type Tray struct {
	hwnd       uintptr
	nid        NOTIFYICONDATAW
	hostname   string
	version    string
	url        string
	running    bool
	statusIcon int
	iconAdded  bool
	mu         sync.RWMutex
	stopCh     chan struct{}

	OnRestartTunnel func()
	OnExit          func()
	OnUpdate        func()
}

func New(hostname, version string) *Tray {
	return &Tray{hostname: hostname, version: version, stopCh: make(chan struct{})}
}

func (t *Tray) SetURL(u string) {
	t.mu.Lock()
	t.url = u
	t.mu.Unlock()
}

func (t *Tray) SetRunning() {
	t.mu.Lock()
	t.running = true
	t.mu.Unlock()
}

func (t *Tray) SetStatusIcon(icon int) {
	t.mu.Lock()
	t.statusIcon = icon
	t.mu.Unlock()
}

func (t *Tray) Run() {
	if runtime.GOOS != "windows" {
		return
	}
	log.Printf("[tray] starting")

	hInstance, _, _ := getModuleHandleW.Call(0)
	className := syscall.StringToUTF16Ptr("Daljinac2Tray")

	cb := syscall.NewCallback(func(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
		return t.wndProc(hwnd, msg, wParam, lParam)
	})

	wc := WNDCLASSEXW{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		LpfnWndProc:   cb,
		HInstance:     hInstance,
		HbrBackground: 6,
		LpszClassName: className,
	}
	reg, _, _ := registerClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if reg == 0 {
		errCode, _, _ := getLastError.Call()
		log.Printf("[tray] RegisterClassExW failed (err=%d)", errCode)
		return
	}

	hwnd, _, _ := createWindowExW.Call(0, uintptr(unsafe.Pointer(className)), 0, 0, 0, 0, 0, 0, 0, 0, hInstance, 0)
	if hwnd == 0 {
		errCode, _, _ := getLastError.Call()
		log.Printf("[tray] CreateWindowExW failed (err=%d)", errCode)
		return
	}
	t.hwnd = hwnd

	tnid := NOTIFYICONDATAW{
		HWnd:             hwnd,
		UID:              100,
		UFlags:           NIF_MESSAGE | NIF_ICON | NIF_TIP,
		UCallbackMessage: WM_APP + 1,
	}
	tnid.CbSize = uint32(unsafe.Sizeof(tnid))
	tnid.HIcon, _, _ = loadIconW.Call(hInstance, IDI_APPLICATION)
	tipStr := "Daljinac2 Agent"
	for i, c := range tipStr {
		if i < 127 {
			tnid.SzTip[i] = uint16(c)
		}
	}
	t.nid = tnid

	add, _, _ := shellNotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(&t.nid)))
	if add == 0 {
		log.Printf("[tray] NIM_ADD failed")
		return
	}
	t.iconAdded = true

	var msg struct {
		HWnd    uintptr
		Message uint32
		WParam  uintptr
		LParam  uintptr
		Time    uint32
		PtX     int32
		PtY     int32
	}

	for {
		select {
		case <-t.stopCh:
			postQuitMessage.Call(0)
			continue
		default:
		}

		ret, _, _ := getMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if ret == 0 {
			break
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		dispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}

	shellNotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&t.nid)))
	destroyWindow.Call(hwnd)
	log.Printf("[tray] stopped")
}

func (t *Tray) Stop() {
	select {
	case <-t.stopCh:
	default:
		close(t.stopCh)
	}
	if t.hwnd != 0 {
		postMessageW.Call(t.hwnd, WM_DESTROY, 0, 0)
	}
}

func (t *Tray) RemoveIcon() {
	if t.iconAdded {
		shellNotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&t.nid)))
		t.iconAdded = false
	}
}

func (t *Tray) wndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_APP + 1:
		if lParam == 0x0205 {
			setForegroundWindow.Call(hwnd)
			menu, _, _ := createPopupMenu.Call()

			title := "Daljinac2 - " + t.hostname
			titlePtr, _ := syscall.UTF16PtrFromString(title)
			appendMenuW.Call(menu, MF_STRING, 200, uintptr(unsafe.Pointer(titlePtr)))

			appendMenuW.Call(menu, MF_SEPARATOR, 201, 0)

			t.mu.RLock()
			u := t.url
			t.mu.RUnlock()

			if u != "" {
				urlLabel := "URL: " + u
				urlPtr, _ := syscall.UTF16PtrFromString(urlLabel)
				appendMenuW.Call(menu, MF_STRING, 202, uintptr(unsafe.Pointer(urlPtr)))
			}

			appendMenuW.Call(menu, MF_SEPARATOR, 203, 0)

			resetPtr, _ := syscall.UTF16PtrFromString("Restart Tunnel")
			appendMenuW.Call(menu, MF_STRING, 204, uintptr(unsafe.Pointer(resetPtr)))

			updatePtr, _ := syscall.UTF16PtrFromString("Check for Updates...")
			appendMenuW.Call(menu, MF_STRING, 205, uintptr(unsafe.Pointer(updatePtr)))

			exitPtr, _ := syscall.UTF16PtrFromString("Exit")
			appendMenuW.Call(menu, MF_STRING, 206, uintptr(unsafe.Pointer(exitPtr)))

			trackPopupMenu.Call(menu, 0, 0, 0, 0, hwnd, 0)
			destroyMenu.Call(menu)
		}
		return 0

	case WM_COMMAND:
		cmd := wParam & 0xFFFF
		switch cmd {
		case 204:
			if t.OnRestartTunnel != nil {
				go t.OnRestartTunnel()
			}
		case 205:
			if t.OnUpdate != nil {
				go t.OnUpdate()
			}
		case 206:
			if t.OnExit != nil {
				go t.OnExit()
			}
		}
		return 0

	case WM_DESTROY:
		postQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := defWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}


