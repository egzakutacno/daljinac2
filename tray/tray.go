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

	registerClassExW  = user32.NewProc("RegisterClassExW")
	createWindowExW   = user32.NewProc("CreateWindowExW")
	defWindowProcW    = user32.NewProc("DefWindowProcW")
	destroyWindow     = user32.NewProc("DestroyWindow")
	postQuitMessage   = user32.NewProc("PostQuitMessage")
	getMessageW       = user32.NewProc("GetMessageW")
	translateMessage  = user32.NewProc("TranslateMessage")
	dispatchMessageW  = user32.NewProc("DispatchMessageW")
	shellNotifyIconW  = user32.NewProc("Shell_NotifyIconW")
	loadIconW         = user32.NewProc("LoadIconW")
	loadCursorW       = user32.NewProc("LoadCursorW")
	createPopupMenu   = user32.NewProc("CreatePopupMenu")
	appendMenuW       = user32.NewProc("AppendMenuW")
	trackPopupMenu    = user32.NewProc("TrackPopupMenu")
	setForegroundWindow = user32.NewProc("SetForegroundWindow")
	postMessage       = user32.NewProc("PostMessageW")
	destroyMenu       = user32.NewProc("DestroyMenu")
	getDC             = user32.NewProc("GetDC")
	releaseDC         = user32.NewProc("ReleaseDC")
	createSolidBrush  = user32.NewProc("CreateSolidBrush")
	fillRect          = user32.NewProc("FillRect")
	deleteObject      = user32.NewProc("DeleteObject")
)

const (
	WM_DESTROY          = 0x0002
	WM_COMMAND          = 0x0111
	WM_USER             = 0x0400
	WM_APP              = 0x8000
	WM_TRAYICON         = WM_APP + 1
	NIN_BALLOONSHOW     = 0x0404
	NIN_BALLOONTIMEOUT  = 0x0405
	NIN_BALLOONUSERCLICK = 0x0406

	NIF_MESSAGE  = 0x00000001
	NIF_ICON     = 0x00000002
	NIF_TIP      = 0x00000004
	NIF_INFO     = 0x00000010
	NIF_SHOWTIP  = 0x00000080

	NOTIFYICON_VERSION_4 = 4

	MF_STRING    = 0x00000000
	MF_SEPARATOR = 0x00000800

	TPM_RIGHTBUTTON = 0x00000002
	TPM_BOTTOMALIGN = 0x00000020

	COLOR_WINDOW = 5

	ID_TRAY_APP    = 100
	ID_MENU_URL    = 1001
	ID_MENU_STATUS = 1002
	ID_MENU_SEP1   = 1003
	ID_MENU_RESET  = 1004
	ID_MENU_UPDATE = 1005
	ID_MENU_SEP2   = 1006
	ID_MENU_EXIT   = 1007

	GCL_HICON      = -14
	IDI_APPLICATION = 32512
	IDC_ARROW       = 32512

	ICON_CONNECTING = 0
	ICON_CONNECTED  = 1
	ICON_ERROR      = 2
)

var IconConnecting = ICON_CONNECTING
var IconConnected = ICON_CONNECTED
var IconError = ICON_ERROR

type Tray struct {
	hostname        string
	version         string
	url             string
	statusIcon      int
	running         bool
	hwnd            uintptr
	mu              sync.Mutex
	stopCh          chan struct{}

	OnURL           func()
	OnRestartTunnel func()
	OnExit          func()
	OnUpdate        func()
}

func New(hostname, version string) *Tray {
	return &Tray{
		hostname:   hostname,
		version:    version,
		statusIcon: ICON_CONNECTING,
		stopCh:     make(chan struct{}),
	}
}

func (t *Tray) SetURL(url string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.url = url
}

func (t *Tray) SetRunning() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.running = true
}

func (t *Tray) SetStatusIcon(icon int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.statusIcon = icon
}

func (t *Tray) Run() {
	if runtime.GOOS != "windows" {
		log.Printf("[tray] skipping (not Windows)")
		return
	}
	log.Printf("[tray] starting")

	hInstance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)

	className, _ := syscall.UTF16PtrFromString("Daljinac2TrayClass")

	wc := struct {
		Size        uint32
		Style       uint32
		WndProc     uintptr
		ClsExtra    int32
		WndExtra    int32
		HInstance   uintptr
		HIcon       uintptr
		HCursor     uintptr
		HbrBg       uintptr
		MenuName    *uint16
		ClassName   *uint16
	}{
		Size:        uint32(unsafe.Sizeof(struct {
			Size, Style, ClsExtra, WndExtra uint32
			WndProc, HInstance, HIcon, HCursor, HbrBg uintptr
			MenuName, ClassName *uint16
		}{}) + 8),
		WndProc:     syscall.NewCallback(t.wndProc),
		HInstance:   hInstance,
		HCursor: func() uintptr { r, _, _ := loadCursorW.Call(0, IDC_ARROW); return r }(),
		HbrBg:       6, // COLOR_WINDOW + 1
		ClassName:   className,
	}

	if _, _, _ = registerClassExW.Call(uintptr(unsafe.Pointer(&wc))); false {
		log.Printf("[tray] RegisterClassExW failed")
	}

	winName, _ := syscall.UTF16PtrFromString("Daljinac2 Agent")
	hwnd, _, _ := createWindowExW.Call(0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(winName)),
		0, 0, 0, 0, 0,
		0, 0, hInstance, 0)

	if hwnd == 0 {
		log.Printf("[tray] CreateWindowExW failed")
		return
	}
	t.hwnd = hwnd

	// Create tray icon
	type trayIconData struct {
		Size       uint32
		HWND       uintptr
		UID        uint32
		Flags      uint32
		Callback   uint32
		HIcon      uintptr
		Tip        [128]uint16
		Info       [256]uint16
		InfoTitle  [64]uint16
		InfoFlags  uint32
		Guid       [16]byte
		BalloonIcon uintptr
	}
	tid := trayIconData{
		HWND:     hwnd,
		UID:      ID_TRAY_APP,
		Flags:    NIF_MESSAGE | NIF_ICON | NIF_TIP | NIF_SHOWTIP,
		Callback: WM_TRAYICON,
	}
	tid.Size = uint32(unsafe.Sizeof(tid))
	tid.HIcon, _, _ = loadIconW.Call(hInstance, IDI_APPLICATION)
	tipStr := "Daljinac2 Agent"
	for i, c := range tipStr {
		if i < 127 {
			tid.Tip[i] = uint16(c)
		}
	}
	shellNotifyIconW.Call(0, 0x00000001, uintptr(unsafe.Pointer(&tid)))

	// Message loop
	var msg struct {
		HWND    uintptr
		Message uint32
		WParam  uintptr
		LParam  uintptr
		Time    uint32
		PtX     int32
		PtY     int32
		Private uint32
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

	shellNotifyIconW.Call(0, 0x00000002, uintptr(unsafe.Pointer(&tid)))
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
		postMessage.Call(t.hwnd, WM_DESTROY, 0, 0)
	}
}

func (t *Tray) RemoveIcon() {
	if t.hwnd != 0 {
		type removeData struct {
			Size uint32
			HWND uintptr
			UID  uint32
			_    uint32
			_    uintptr
			_    [128]uint16
		}
		rd := removeData{
			HWND: t.hwnd,
			UID:  ID_TRAY_APP,
		}
		rd.Size = uint32(unsafe.Sizeof(rd))
		shellNotifyIconW.Call(0, 0x00000002, uintptr(unsafe.Pointer(&rd)))
	}
}

func (t *Tray) wndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_TRAYICON:
		if lParam == 0x0205 { // WM_CONTEXTMENU via shell
			setForegroundWindow.Call(hwnd)
			menu, _, _ := createPopupMenu.Call()

			t.mu.Lock()
			url := t.url
			statusIcon := t.statusIcon
			t.mu.Unlock()

			statusText := "Connected"
			if statusIcon == ICON_CONNECTING {
				statusText = "Connecting..."
			} else if statusIcon == ICON_ERROR {
				statusText = "Error"
			}

			label := "Daljinac2 - " + t.hostname
			labelPtr, _ := syscall.UTF16PtrFromString(label)
			appendMenuW.Call(menu, MF_STRING, ID_MENU_STATUS, uintptr(unsafe.Pointer(labelPtr)))

			appendMenuW.Call(menu, MF_SEPARATOR, ID_MENU_SEP1, 0)

			if url != "" {
				urlLabel := "URL: " + url
				urlPtr, _ := syscall.UTF16PtrFromString(urlLabel)
				appendMenuW.Call(menu, MF_STRING, ID_MENU_URL, uintptr(unsafe.Pointer(urlPtr)))
			}

			statusLabel := "Status: " + statusText
			statusPtr, _ := syscall.UTF16PtrFromString(statusLabel)
			appendMenuW.Call(menu, MF_STRING, ID_MENU_STATUS+1, uintptr(unsafe.Pointer(statusPtr)))

			appendMenuW.Call(menu, MF_SEPARATOR, ID_MENU_SEP2, 0)

			resetPtr, _ := syscall.UTF16PtrFromString("Restart Tunnel")
			appendMenuW.Call(menu, MF_STRING, ID_MENU_RESET, uintptr(unsafe.Pointer(resetPtr)))

			updatePtr, _ := syscall.UTF16PtrFromString("Check for Updates...")
			appendMenuW.Call(menu, MF_STRING, ID_MENU_UPDATE, uintptr(unsafe.Pointer(updatePtr)))

			exitPtr, _ := syscall.UTF16PtrFromString("Exit")
			appendMenuW.Call(menu, MF_STRING, ID_MENU_EXIT, uintptr(unsafe.Pointer(exitPtr)))

			trackPopupMenu.Call(menu, TPM_RIGHTBUTTON|TPM_BOTTOMALIGN, 0, 0, 0, hwnd, 0)
			destroyMenu.Call(menu)
		}
		return 0

	case WM_COMMAND:
		cmd := wParam & 0xFFFF
		switch cmd {
		case ID_MENU_RESET:
			if t.OnRestartTunnel != nil {
				go t.OnRestartTunnel()
			}
		case ID_MENU_UPDATE:
			if t.OnUpdate != nil {
				go t.OnUpdate()
			}
		case ID_MENU_EXIT:
			if t.OnExit != nil {
				t.OnExit()
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
