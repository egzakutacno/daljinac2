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

	"github.com/egzakutacno/daljinac2/internal/auth"
	"github.com/egzakutacno/daljinac2/internal/tunnel"
	daljinacmcp "github.com/egzakutacno/daljinac2/internal/mcp"
	"github.com/egzakutacno/daljinac2/tray"
)

const version = "2.0.0-dev"
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

	port := flag.Int("port", mcpPort, "MCP server port")
	noTray := flag.Bool("notray", false, "No system tray")
	flag.Parse()

	args := flag.Args()
	if len(args) > 0 && args[0] == "-install" {
		doInstall()
		return
	}
	if len(args) > 0 && args[0] == "-remove" {
		doRemove()
		return
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		log.Printf("Port %d in use — another instance running. Exiting.", *port)
		return
	}
	ln.Close()

	shutdown := make(chan struct{})
	hostname, _ := os.Hostname()
	log.Printf("Hostname: %s, Version: %s, Port: %d, Tray: %v", hostname, version, *port, !*noTray)

	// Create MCP server
	mcpServer := daljinacmcp.NewDMCPServer(version)
	httpHandler := daljinacmcp.NewStreamableHTTPServer(mcpServer, "/mcp")
	// Wrap with auth
	authHandler := auth.Middleware(httpHandler)

	// Setup direct HTTP access too (for tunnel health checks)
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
		close(shutdown)
	}

	onConnect := func(url string) {
		tr.SetURL(url)
		tr.SetRunning()
		tr.SetStatusIcon(tray.IconConnected)
	}

	if !*noTray {
		time.Sleep(3 * time.Second)
		go tr.Run()
	} else {
		log.Println("Headless mode")
	}

	// Start MCP HTTP server
	go func() {
		defer func() { recover() }()
		addr := fmt.Sprintf(":%d", *port)
		log.Printf("Starting MCP Streamable HTTP server on %s", addr)
		if err := http.ListenAndServe(addr, directMux); err != nil {
			log.Printf("HTTP error: %v", err)
		}
	}()

	// Start SSH tunnel
	t := tunnel.NewSSH(*port, hostname, onConnect)
	t.Start()

	// Watchdog: exit if tunnel not connected for 30 minutes
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			if t == nil {
				continue
			}
			since := time.Since(t.LastConnected())
			if since > 30*time.Minute {
				log.Printf("[watchdog] tunnel not connected for %v, exiting", since)
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
	case <-sig:
	}

	log.Println("Shutdown")
	if t != nil {
		t.Stop()
	}
	tr.Stop()
}

func doInstall() {
	exe, _ := os.Executable()
	os.MkdirAll("C:\\daljinac2", 0755)
	vbs := "CreateObject(\"WScript.Shell\").Run \"schtasks /run /tn Daljinac2\", 0, False\n"
	os.WriteFile("C:\\daljinac2\\watchdog.vbs", []byte(vbs), 0644)
	exec.Command("schtasks", "/delete", "/tn", "Daljinac2", "/f").Run()
	exec.Command("schtasks", "/delete", "/tn", "Daljinac2Watch", "/f").Run()
	exec.Command("schtasks", "/create", "/tn", "Daljinac2", "/tr", exe, "/sc", "ONLOGON", "/rl", "HIGHEST", "/f").Run()
	exec.Command("schtasks", "/create", "/tn", "Daljinac2Watch", "/tr", "wscript.exe //B C:\\daljinac2\\watchdog.vbs", "/sc", "MINUTE", "/mo", "5", "/f").Run()
	exec.Command("cmd", "/c", "start", "", "/min", exe).Run()
	log.Println("Installed (scheduled task + watchdog)")
}

func doRemove() {
	exec.Command("taskkill", "/f", "/im", "daljinac2.exe").Run()
	exec.Command("schtasks", "/delete", "/tn", "Daljinac2", "/f").Run()
	exec.Command("schtasks", "/delete", "/tn", "Daljinac2Watch", "/f").Run()
	os.Remove("C:\\daljinac2\\watchdog.vbs")
	log.Println("Removed")
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
	log.Printf("Downloading %s", dlURL)
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

	log.Println("Update launched, exiting")
	os.Exit(0)
	return nil
}
