package shell

import (
	"context"
	"os"
	"strings"
	"time"
)

var extDirs = []string{
	"C:\\Windows\\System32",
	"C:\\Windows",
	"C:\\Windows\\System32\\Wbem",
	"C:\\Windows\\System32\\WindowsPowerShell\\v1.0",
}

func ShellNative(command string, timeout time.Duration) Result {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return CMD(command, timeout)
	}
	exe := parts[0]
	args := parts[1:]

	if isPSCommand(exe) {
		return PowerShell(command, timeout)
	}

	path := findExe(exe)
	if path == "" {
		return CMD(command, timeout)
	}

	return executeDirect(path, args, timeout)
}

func executeDirect(path string, args []string, timeout time.Duration) Result {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return execute(ctx, path, args...)
}

func findExe(name string) string {
	if strings.Contains(name, "\\") || strings.Contains(name, "/") {
		if _, err := os.Stat(name); err == nil {
			return name
		}
		return ""
	}
	nameLower := strings.ToLower(name)
	if !strings.Contains(nameLower, ".") {
		for _, ext := range []string{".exe", ".com", ".bat", ".cmd"} {
			path := findInDirs(name + ext)
			if path != "" {
				return path
			}
		}
	} else {
		path := findInDirs(name)
		if path != "" {
			return path
		}
	}
	return ""
}

func findInDirs(name string) string {
	for _, d := range extDirs {
		path := d + "\\" + name
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func isPSCommand(name string) bool {
	prefixes := []string{
		"get-", "set-", "new-", "remove-", "start-", "stop-", "restart-",
		"invoke-", "out-", "write-", "format-", "select-", "where-",
		"sort-", "group-", "measure-", "import-", "export-", "convertto-",
		"foreach-", "test-", "enable-", "disable-", "add-", "rename-",
		"copy-", "move-", "clear-", "compare-", "connect-", "convert-",
		"debug-", "disconnect-", "enter-", "exit-", "expand-",
		"get-", "join-", "limit-", "pop-", "push-", "read-",
		"receive-", "register-", "resolve-", "resume-", "save-",
		"send-", "show-", "split-", "suspend-", "switch-", "tee-",
		"trace-", "undo-", "unregister-", "update-", "wait-",
	}
	lower := strings.ToLower(name)
	for _, p := range prefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}
