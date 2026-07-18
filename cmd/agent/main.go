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

const version = "2.0.0-dev.20260718.2"
const maxLogSize = 1 * 1024 * 1024
const mcpPort = 1984

var logFile *os.File

func initLog() {
	logDir := filepath.Join(os.Getenv("ProgramData"), "daljinac2")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		logDir = "C:\\daljinac2"
		os.MkdirAll(logDir, 0755)
	}
	logPath := filepath.Join(logDir, "daljinac2.log")
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
	marker := "C:\\daljinac2\\started.txt"
	os.MkdirAll("C:\\daljinac2", 0755)
	os.WriteFile(marker, []byte(time.Now().Format(time.RFC3339)+"\n"), 0644)
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
	directMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"agent":"daljinac2","version":"%s","status":"running"}`, version)
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

	// Watchdog: exit if tunnel not connected for 30 minutes
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			if t == nil {
				continue
			}
			since := time.Since(t.LastConnected())
			log.Printf("[watchdog] check: last connected %v ago", since)
			if since > 30*time.Minute {
				log.Printf("[watchdog] tunnel not connected for %v, exiting (task will restart)", since)
				syncLog()
				os.Exit(1)
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

	os.MkdirAll("C:\\daljinac2", 0755)

	// Write watchdog VBS
	vbs := "CreateObject(\"WScript.Shell\").Run \"schtasks /run /tn Daljinac2\", 0, False\n"
	if err := os.WriteFile("C:\\daljinac2\\watchdog.vbs", []byte(vbs), 0644); err != nil {
		log.Printf("[install] write watchdog.vbs failed: %v", err)
	} else {
		log.Println("[install] watchdog.vbs written")
	}

	// Remove old tasks first
	logExec("schtasks", "/delete", "/tn", "Daljinac2", "/f")
	logExec("schtasks", "/delete", "/tn", "Daljinac2Watch", "/f")

	// Create main task with /it (Interactive) flag so tray works
	if err := logExec("schtasks", "/create", "/tn", "Daljinac2", "/tr", exe,
		"/sc", "ONLOGON", "/it", "/rl", "HIGHEST", "/f"); err != nil {
		log.Printf("[install] FAILED to create Daljinac2 task: %v", err)
	} else {
		log.Println("[install] Daljinac2 task created")
	}

	// Create watchdog task
	if err := logExec("schtasks", "/create", "/tn", "Daljinac2Watch",
		"/tr", "wscript.exe //B C:\\daljinac2\\watchdog.vbs",
		"/sc", "MINUTE", "/mo", "5", "/f"); err != nil {
		log.Printf("[install] FAILED to create Daljinac2Watch task: %v", err)
	} else {
		log.Println("[install] Daljinac2Watch task created")
	}

	// Add Run registry key as fallback
	if k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Run`,
		registry.SET_VALUE); err == nil {
		if err := k.SetStringValue("Daljinac2", `"`+exe+`"`); err != nil {
			log.Printf("[install] registry Run key set failed: %v", err)
		} else {
			log.Println("[install] Run registry key set")
		}
		k.Close()
	} else {
		log.Printf("[install] cannot open Run registry key: %v", err)
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
	logExec("taskkill", "/f", "/im", "daljinac2.exe")
	logExec("schtasks", "/delete", "/tn", "Daljinac2", "/f")
	logExec("schtasks", "/delete", "/tn", "Daljinac2Watch", "/f")

	if k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Run`,
		registry.SET_VALUE); err == nil {
		k.DeleteValue("Daljinac2")
		k.Close()
	}

	os.Remove("C:\\daljinac2\\watchdog.vbs")
	log.Println("[remove] done")
	syncLog()
}

func updateURL() string {
	name := filepath.Base(os.Args[0])
	return "https://github.com/egzakutacno/daljinac2/releases/latest/download/" + name
}

func doUpdate() error {
	tmpDir := filepath.Join(os.TempDir(), "daljinac2-update")
	os.MkdirAll(tmpDir, 0755)

	dlURL := updateURL()
	newExe := filepath.Join(tmpDir, filepath.Base(os.Args[0]))
	log.Printf("[update] downloading %s", dlURL)
	resp, err := http.Get(dlURL)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	out, _ := os.Create(newExe)
	io.Copy(out, resp.Body)
	out.Close()

	current, _ := os.Executable()

	argsFile := filepath.Join(tmpDir, "args.txt")
	fullCmd := fmt.Sprintf(`"%s" %s`, current, strings.Join(os.Args[1:], " "))
	os.WriteFile(argsFile, []byte(fullCmd), 0644)

	logFile := filepath.Join(tmpDir, "update.log")
	bat := filepath.Join(tmpDir, "up.bat")
	batch := fmt.Sprintf(`@echo off
set LOG="%s"
echo %%date%% %%time%% [update] starting >> %%LOG%%
set /p CMD=<"%s"
echo %%date%% %%time%% [update] copying new binary >> %%LOG%%
copy /y "%s" "%s" >> %%LOG%% 2>&1
if %%errorlevel%% neq 0 (
    echo %%date%% %%time%% [update] COPY FAILED >> %%LOG%%
    exit /b 1
)
echo %%date%% %%time%% [update] copy OK, killing >> %%LOG%%
taskkill /f /im daljinac2.exe >> %%LOG%% 2>&1
timeout /t 2 /nobreak > nul
echo %%date%% %%time%% [update] writing vbs >> %%LOG%%
echo CreateObject("WScript.Shell").Run "schtasks /run /tn Daljinac2", 0, False > C:\daljinac2\watchdog.vbs
echo %%date%% %%time%% [update] registering tasks >> %%LOG%%
schtasks /delete /tn Daljinac2 /f >> %%LOG%% 2>&1
schtasks /create /tn Daljinac2 /tr "%%CMD%%" /sc ONLOGON /rl HIGHEST /f >> %%LOG%% 2>&1
echo %%date%% %%time%% [update] starting app >> %%LOG%%
schtasks /run /tn Daljinac2 >> %%LOG%% 2>&1
for /l %%i in (1,1,3) do (
  timeout /t 5 /nobreak > nul
  tasklist /fi "imagename eq daljinac2.exe" 2>nul | find /i "daljinac2" >nul
  if not errorlevel 1 goto RUNNING
  start "" /min %%CMD%% >> %%LOG%% 2>&1
)
:RUNNING
schtasks /delete /tn Daljinac2Watch /f >> %%LOG%% 2>&1
schtasks /create /tn Daljinac2Watch /tr "wscript.exe //B C:\daljinac2\watchdog.vbs" /sc MINUTE /mo 5 /f >> %%LOG%% 2>&1
echo %%date%% %%time%% [update] done >> %%LOG%%
del "%s"
del "%%~f0"
`, logFile, argsFile, newExe, current, argsFile)
	os.WriteFile(bat, []byte(batch), 0644)

	shell32 := syscall.NewLazyDLL("shell32.dll")
	se := shell32.NewProc("ShellExecuteW")
	se.Call(0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("runas"))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("cmd"))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("/C \""+bat+"\""))),
		0, 0)

	log.Println("[update] launched, exiting")
	syncLog()
	os.Exit(0)
	return nil
}
