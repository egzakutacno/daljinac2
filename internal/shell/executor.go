package shell

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
)

type Result struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

func execute(ctx context.Context, name string, args ...string) Result {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	exitCode := 0
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return Result{
				Stderr:   err.Error(),
				ExitCode: -1,
			}
		}
	}

	return Result{
		Stdout:   strings.TrimSpace(stdout.String()),
		Stderr:   strings.TrimSpace(stderr.String()),
		ExitCode: exitCode,
	}
}

func CMD(command string, timeout time.Duration) Result {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return execute(ctx, "cmd", "/C", command)
}

func PowerShell(command string, timeout time.Duration) Result {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	encoded := encodePS(command)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return execute(ctx, "powershell", "-EncodedCommand", encoded)
}

func CMDEx(command string, timeout time.Duration) Result {
	return CMD(command, timeout)
}

func encodePS(cmd string) string {
	runes := []rune(cmd)
	utf16le := utf16.Encode(runes)
	buf := make([]byte, len(utf16le)*2)
	for i, r := range utf16le {
		buf[i*2] = byte(r)
		buf[i*2+1] = byte(r >> 8)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

func Shell(command string, timeout time.Duration) Result {
	lower := strings.ToLower(strings.TrimSpace(command))
	if strings.HasPrefix(lower, "powershell") || strings.HasPrefix(lower, "ps ") {
		return PowerShell(strings.TrimPrefix(strings.TrimPrefix(command, "powershell "), "ps "), timeout)
	}
	return CMD(command, timeout)
}

func DetectShell(command string) string {
	lower := strings.ToLower(strings.TrimSpace(command))
	if strings.HasPrefix(lower, "powershell") || strings.HasPrefix(lower, "ps ") {
		return "powershell"
	}
	return "cmd"
}

func RunDetected(command string, timeout time.Duration) (Result, string) {
	shellType := DetectShell(command)
	cleaned := strings.TrimSpace(command)
	if shellType == "powershell" {
		cleaned = strings.TrimPrefix(cleaned, "powershell ")
		cleaned = strings.TrimPrefix(cleaned, "ps ")
		return PowerShell(cleaned, timeout), "powershell"
	}
	return CMD(cleaned, timeout), "cmd"
}

func MustParseTimeout(timeoutStr string) time.Duration {
	if timeoutStr == "" {
		return 30 * time.Second
	}
	d, err := time.ParseDuration(timeoutStr)
	if err != nil {
		return 30 * time.Second
	}
	if d <= 0 || d > 300*time.Second {
		return 30 * time.Second
	}
	return d
}

func FormatResult(r Result) string {
	var parts []string
	if r.Stdout != "" {
		parts = append(parts, fmt.Sprintf("stdout:\n%s", r.Stdout))
	}
	if r.Stderr != "" {
		parts = append(parts, fmt.Sprintf("stderr:\n%s", r.Stderr))
	}
	parts = append(parts, fmt.Sprintf("exit_code: %d", r.ExitCode))
	return strings.Join(parts, "\n")
}
