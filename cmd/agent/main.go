package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows/registry"

	"github.com/egzakutacno/daljinac2/internal/auth"
	"github.com/egzakutacno/daljinac2/internal/tunnel"
	daljinacmcp "github.com/egzakutacno/daljinac2/internal/mcp"
	"github.com/egzakutacno/daljinac2/tray"
)

const version = "2.0.0-dev.20260721.6"
const maxLogSize = 1 * 1024 * 1024
const mcpPort = 1984
const originalExeName = "daljinac2.exe"

var logFile *os.File

func exeDir() string {
	exe, _ := os.Executable()
	return filepath.Dir(exe)
}

func exeBase() string {
	return strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
}

func initLog() {
	logDir := exeDir()
	os.MkdirAll(logDir, 0755)
	logPath := filepath.Join(logDir, exeBase()+".log")
	if fi, err := os.Stat(logPath); err == nil && fi.Size() > maxLogSize {
		os.Rename(logPath, logPath+".old")
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("WARN: cannot open log %s: %v", logPath, err)
		return
	}
	logFile = f
	log.SetOutput(f)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Printf("=== daljinac2 v%s starting ===", version)
	syncLog()
}

func syncLog() {
	if logFile != nil {
		logFile.Sync()
	}
}

func hideConsole() {
	if runtime.GOOS != "windows" {
		return
	}
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getConsoleWindow := kernel32.NewProc("GetConsoleWindow")
	user32 := syscall.NewLazyDLL("user32.dll")
	showWindow := user32.NewProc("ShowWindow")
	hwnd, _, _ := getConsoleWindow.Call()
	if hwnd != 0 {
		showWindow.Call(hwnd, 0)
	}
}

func writeStartupMarker() {
	dir := exeDir()
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "started.txt"), []byte(time.Now().Format(time.RFC3339)+"\n"), 0644)
}

func logExec(cmdName string, args ...string) error {
	cmd := exec.Command(cmdName, args...)
	out, err := cmd.CombinedOutput()
	outStr := strings.TrimSpace(string(out))
	if err != nil {
		log.Printf("[exec] FAILED: %s %v | output: %s | err: %v", cmdName, args, outStr, err)
	} else if outStr != "" {
		log.Printf("[exec] OK: %s %v | output: %s", cmdName, args, outStr)
	} else {
		log.Printf("[exec] OK: %s %v", cmdName, args)
	}
	syncLog()
	return err
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			b := make([]byte, 4096)
			n := runtime.Stack(b, true)
			log.Printf("PANIC: %v\n%s", r, b[:n])
			syncLog()
		}
	}()
	writeStartupMarker()
	initLog()
	hideConsole()
	time.Sleep(2 * time.Second)

	// Check os.Args for -install / -remove BEFORE flag.Parse, because
	// these are not defined flags and flag.Parse would exit with code 2.
	for _, a := range os.Args {
		if a == "-install" {
			doInstall()
			syncLog()
			return
		}
		if a == "-remove" {
			doRemove()
			syncLog()
			return
		}
	}

	port := flag.Int("port", mcpPort, "MCP server port")
	noTray := flag.Bool("notray", false, "No system tray")
	flag.Parse()

	log.Printf("=== daljinac2 v%s process start ===", version)
	log.Printf("Args: %v", os.Args)
	syncLog()

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		log.Printf("Port %d in use - another instance running. Exiting.", *port)
		syncLog()
		return
	}
	ln.Close()

	shutdown := make(chan struct{})
	hostname, _ := os.Hostname()
	log.Printf("Hostname: %s, Version: %s, Port: %d, Tray: %v", hostname, version, *port, !*noTray)
	syncLog()

	// Create MCP server
	mcpServer := daljinacmcp.NewDMCPServer(version)
	httpHandler := daljinacmcp.NewStreamableHTTPServer(mcpServer, "/mcp")
	authHandler := auth.Middleware(httpHandler)

	directMux := http.NewServeMux()
	directMux.Handle("/mcp", authHandler)
	directMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"running","version":"%s"}`, version)
	})
	directMux.HandleFunc("/api/update", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"update started"}`)
		go func() {
			if err := doUpdate(); err != nil {
				log.Printf("[update] error: %v", err)
			}
		}()
	})
	directMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"agent":"%s","version":"%s","status":"running"}`, exeBase(), version)
	})

	// Setup tray
	tr := tray.New(hostname, version)
	tr.OnRestartTunnel = func() {
		log.Println("[main] restarting tunnel not supported yet")
	}
	tr.OnExit = func() {
		log.Println("[main] OnExit triggered - shutting down")
		syncLog()
		close(shutdown)
	}

	onConnect := func(url string) {
		log.Printf("[main] tunnel connected: %s", url)
		syncLog()
		tr.SetURL(url)
		tr.SetRunning()
		tr.SetStatusIcon(tray.IconConnected)
		bakPath := filepath.Join(exeDir(), "daljinac2.exe.bak")
		if _, err := os.Stat(bakPath); err == nil {
			os.Remove(bakPath)
			log.Println("[main] update successful, removed .bak backup")
			syncLog()
		}
	}

	if !*noTray {
		time.Sleep(3 * time.Second)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[tray] PANIC: %v", r)
					syncLog()
				}
			}()
			log.Println("[main] starting tray goroutine")
			syncLog()
			tr.Run()
			log.Println("[main] tray goroutine exited")
			syncLog()
		}()
	} else {
		log.Println("Headless mode")
		syncLog()
	}

	// Start MCP HTTP server
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[http] PANIC: %v", r)
				syncLog()
			}
		}()
		addr := fmt.Sprintf(":%d", *port)
		log.Printf("Starting MCP Streamable HTTP server on %s", addr)
		syncLog()
		if err := http.ListenAndServe(addr, directMux); err != nil {
			log.Printf("[http] server error: %v", err)
			syncLog()
		}
	}()

	// Start SSH tunnel
	t := tunnel.NewSSH(*port, hostname, onConnect)
	t.Start()
	log.Println("[main] SSH tunnel started")
	syncLog()

	// Watchdog: monitor tunnel health, don't kill agent
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			if t == nil {
				continue
			}
			since := time.Since(t.LastConnected())
			if since > 30*time.Minute {
				log.Printf("[watchdog] WARNING: tunnel not connected for %v (will keep retrying)", since)
				syncLog()
			}
		}
	}()

	if *noTray {
		select {}
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-shutdown:
		log.Println("[main] shutdown via OnExit")
	case s := <-sig:
		log.Printf("[main] shutdown via signal: %v", s)
	}
	syncLog()

	log.Println("[main] cleaning up...")
	syncLog()
	if t != nil {
		t.Stop()
	}
	tr.Stop()
	log.Println("[main] done")
	syncLog()
}

func doInstall() {
	exe, err := os.Executable()
	if err != nil {
		log.Printf("[install] os.Executable failed: %v", err)
		return
	}
	log.Printf("[install] binary: %s", exe)

	dir := exeDir()
	base := exeBase()
	taskName := base
	watchName := base + "Watch"
	os.MkdirAll(dir, 0755)

	// Write watchdog VBS
	vbs := fmt.Sprintf("CreateObject(\"WScript.Shell\").Run \"schtasks /run /tn %s\", 0, False\n", taskName)
	vbsPath := filepath.Join(dir, "watchdog.vbs")
	if err := os.WriteFile(vbsPath, []byte(vbs), 0644); err != nil {
		log.Printf("[install] write watchdog.vbs failed: %v", err)
	} else {
		log.Println("[install] watchdog.vbs written")
	}

	// Remove old tasks first
	logExec("schtasks", "/delete", "/tn", taskName, "/f")
	logExec("schtasks", "/delete", "/tn", watchName, "/f")

	// Create main task with /it (Interactive) flag so tray works
	if err := logExec("schtasks", "/create", "/tn", taskName, "/tr", exe,
		"/sc", "ONLOGON", "/it", "/rl", "HIGHEST", "/f"); err != nil {
		log.Printf("[install] FAILED to create %s task: %v", taskName, err)
	} else {
		log.Printf("[install] %s task created", taskName)
	}

	// Create watchdog task
	if err := logExec("schtasks", "/create", "/tn", watchName,
		"/tr", fmt.Sprintf("wscript.exe //B %s", vbsPath),
		"/sc", "MINUTE", "/mo", "5", "/f"); err != nil {
		log.Printf("[install] FAILED to create %s task: %v", watchName, err)
	} else {
		log.Printf("[install] %s task created", watchName)
	}

	// Add Run registry key as fallback
	if k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Run`,
		registry.SET_VALUE); err == nil {
		if err := k.SetStringValue(base, `"`+exe+`"`); err != nil {
			log.Printf("[install] registry Run key set failed: %v", err)
		} else {
			log.Println("[install] Run registry key set")
		}
		k.Close()
	}

	syncLog()

	// Start the app
	if err := logExec("cmd", "/c", "start", "", "/min", exe); err != nil {
		log.Printf("[install] start app failed: %v", err)
	} else {
		log.Println("[install] app started")
	}
	syncLog()
}

func doRemove() {
	exeName := filepath.Base(os.Args[0])
	base := exeBase()
	taskName := base
	watchName := base + "Watch"
	logExec("taskkill", "/f", "/im", exeName)
	logExec("schtasks", "/delete", "/tn", taskName, "/f")
	logExec("schtasks", "/delete", "/tn", watchName, "/f")

	if k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Run`,
		registry.SET_VALUE); err == nil {
		k.DeleteValue(base)
		k.Close()
	}

	os.Remove(filepath.Join(exeDir(), "watchdog.vbs"))
	log.Println("[remove] done")
	syncLog()
}

func updateURL() string {
	return "https://github.com/egzakutacno/daljinac2/releases/latest/download/" + originalExeName
}

func doUpdate() error {
	base := exeBase()

	tmpDir := filepath.Join(os.TempDir(), base+"-update")
	os.MkdirAll(tmpDir, 0755)

	dlURL := updateURL()
	newExe := filepath.Join(tmpDir, originalExeName)
	log.Printf("[update] downloading %s", dlURL)
	resp, err := http.Get(dlURL)
	if err != nil {
		log.Printf("[update] download failed: %v, retrying once...", err)
		time.Sleep(5 * time.Second)
		resp, err = http.Get(dlURL)
	}
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	out, err := os.Create(newExe)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	n, err := io.Copy(out, resp.Body)
	out.Close()
	if err != nil {
		return fmt.Errorf("download incomplete (%d bytes): %w", n, err)
	}
	if n < 1024*1024 {
		return fmt.Errorf("downloaded binary too small: %d bytes", n)
	}
	header := make([]byte, 2)
	f, err := os.Open(newExe)
	if err == nil {
		f.Read(header)
		f.Close()
	}
	if header[0] != 'M' || header[1] != 'Z' {
		return fmt.Errorf("downloaded file is not a valid PE executable")
	}

	current, _ := os.Executable()
	args := strings.Join(os.Args[1:], " ")

	vbsContent := fmt.Sprintf(`On Error Resume Next
Set ws = CreateObject("WScript.Shell")
Set fso = CreateObject("Scripting.FileSystemObject")
WScript.Sleep 3000
fso.CopyFile "%s", "%s", True
If Err.Number = 0 Then
  ws.Run """%s"" %s", 0, False
End If
`, newExe, current, current, args)
	vbsPath := filepath.Join(tmpDir, "up.vbs")
	os.WriteFile(vbsPath, []byte(vbsContent), 0644)

	log.Printf("[update] VBS: %s", vbsPath)
	log.Printf("[update] saving %s -> %s", newExe, current)

	shell32 := syscall.NewLazyDLL("shell32.dll")
	se := shell32.NewProc("ShellExecuteW")
	se.Call(0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("open"))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("wscript.exe"))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("/B \""+vbsPath+"\""))),
		0, 0)

	log.Println("[update] launched, exiting")
	syncLog()
	os.Exit(0)
	return nil
}
