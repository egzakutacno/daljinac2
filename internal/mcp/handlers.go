package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/egzakutacno/daljinac2/internal/files"
	"github.com/egzakutacno/daljinac2/internal/input"
	"github.com/egzakutacno/daljinac2/internal/screen"
	"github.com/egzakutacno/daljinac2/internal/shell"
	"github.com/egzakutacno/daljinac2/internal/system"
)

var log *zap.SugaredLogger

func init() {
	cfg := zap.NewProductionConfig()
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.Level.SetLevel(zapcore.InfoLevel)
	l, _ := cfg.Build()
	log = l.Sugar()
}

func registerScreenTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("screenshot",
		mcp.WithDescription("Capture remote desktop screenshot as PNG. Use this to see what's on the screen"),
		mcp.WithNumber("monitor",
			mcp.Description("Monitor index (0 = primary, default: 0)"),
		),
		mcp.WithNumber("max_width",
			mcp.Description("Resize to max width while preserving aspect ratio (0 = full resolution)"),
		),
		mcp.WithNumber("quality",
			mcp.Description("JPEG quality 1-100 (default: 80, only used if format=jpeg)"),
		),
		mcp.WithString("format",
			mcp.Description("Image format: png (default) or jpeg"),
			mcp.Enum("png", "jpeg"),
		),
	), handleScreenshot)

	s.AddTool(mcp.NewTool("screenshot_base64",
		mcp.WithDescription("Capture remote desktop screenshot as base64-encoded string (for inline display)"),
		mcp.WithNumber("monitor",
			mcp.Description("Monitor index (0 = primary, default: 0)"),
		),
		mcp.WithNumber("max_width",
			mcp.Description("Resize to max width (0 = full resolution, default: 1280)"),
		),
		mcp.WithNumber("quality",
			mcp.Description("JPEG quality 1-100 (default: 40, smaller = faster to transmit)"),
		),
	), handleScreenshotBase64)

	s.AddTool(mcp.NewTool("num_monitors",
		mcp.WithDescription("Get the number of active monitors/displays"),
	), handleNumMonitors)
}

func registerInputTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("mouse_move",
		mcp.WithDescription("Move mouse cursor to absolute screen coordinates"),
		mcp.WithNumber("x", mcp.Required(), mcp.Description("X coordinate (pixels)")),
		mcp.WithNumber("y", mcp.Required(), mcp.Description("Y coordinate (pixels)")),
	), handleMouseMove)

	s.AddTool(mcp.NewTool("mouse_click",
		mcp.WithDescription("Click mouse at specified coordinates"),
		mcp.WithNumber("x", mcp.Required(), mcp.Description("X coordinate")),
		mcp.WithNumber("y", mcp.Required(), mcp.Description("Y coordinate")),
		mcp.WithString("button",
			mcp.Description("Mouse button: left (default), right, middle"),
			mcp.Enum("left", "right", "middle"),
		),
		mcp.WithString("click_type",
			mcp.Description("Click type: single (default), double"),
			mcp.Enum("single", "double"),
		),
	), handleMouseClick)

	s.AddTool(mcp.NewTool("mouse_scroll",
		mcp.WithDescription("Scroll at a position on the screen"),
		mcp.WithNumber("x", mcp.Required(), mcp.Description("X coordinate")),
		mcp.WithNumber("y", mcp.Required(), mcp.Description("Y coordinate")),
		mcp.WithNumber("delta_x", mcp.Description("Horizontal scroll amount (positive=right, negative=left)")),
		mcp.WithNumber("delta_y", mcp.Description("Vertical scroll amount (positive=down, negative=up)")),
	), handleMouseScroll)

	s.AddTool(mcp.NewTool("mouse_drag",
		mcp.WithDescription("Click and drag from one position to another"),
		mcp.WithNumber("from_x", mcp.Required(), mcp.Description("Starting X coordinate")),
		mcp.WithNumber("from_y", mcp.Required(), mcp.Description("Starting Y coordinate")),
		mcp.WithNumber("to_x", mcp.Required(), mcp.Description("Ending X coordinate")),
		mcp.WithNumber("to_y", mcp.Required(), mcp.Description("Ending Y coordinate")),
	), handleMouseDrag)

	s.AddTool(mcp.NewTool("keyboard_type",
		mcp.WithDescription("Type text using keyboard simulation (Unicode)"),
		mcp.WithString("text", mcp.Required(), mcp.Description("Text to type")),
	), handleKeyboardType)

	s.AddTool(mcp.NewTool("keyboard_hotkey",
		mcp.WithDescription("Press keyboard shortcut combination (e.g. Ctrl+C, Win+R)"),
		mcp.WithArray("keys",
			mcp.Required(),
			mcp.Description("Array of key names: ctrl, alt, shift, win, a-z, 0-9, f1-f12, enter, tab, escape, etc."),
			mcp.WithStringItems(),
		),
	), handleKeyboardHotkey)
}

func registerShellTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("shell",
		mcp.WithDescription("Execute a command via PowerShell (recommended) or CMD on the remote Windows machine. Returns stdout, stderr, and exit code. Prefix with 'powershell' or 'ps' to force PowerShell execution."),
		mcp.WithString("command", mcp.Required(), mcp.Description("Command to execute")),
		mcp.WithNumber("timeout", mcp.Description("Timeout in seconds (default: 30, max: 300)")),
	), handleShell)
}

func registerFileTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("file_read",
		mcp.WithDescription("Read a file from the remote machine (up to 10MB)"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute path to the file")),
		mcp.WithString("encoding",
			mcp.Description("Output encoding: text (default) or base64"),
			mcp.Enum("text", "base64"),
		),
	), handleFileRead)

	s.AddTool(mcp.NewTool("file_write",
		mcp.WithDescription("Write content to a file on the remote machine"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute path to write to")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Content to write")),
	), handleFileWrite)

	s.AddTool(mcp.NewTool("file_list",
		mcp.WithDescription("List files in a directory on the remote machine"),
		mcp.WithString("dir", mcp.Description("Directory path (default: C:\\)")),
	), handleFileList)
}

func registerSystemTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("processes",
		mcp.WithDescription("List running processes on the remote machine"),
	), handleProcesses)

	s.AddTool(mcp.NewTool("clipboard_get",
		mcp.WithDescription("Get text content from the remote clipboard"),
	), handleClipboardGet)

	s.AddTool(mcp.NewTool("clipboard_set",
		mcp.WithDescription("Set text content on the remote clipboard"),
		mcp.WithString("text", mcp.Required(), mcp.Description("Text to set on clipboard")),
	), handleClipboardSet)

	s.AddTool(mcp.NewTool("window_list",
		mcp.WithDescription("List open windows on the remote desktop (title, class, position, visibility)"),
	), handleWindowList)
}

func registerInfoTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("get_screen_size",
		mcp.WithDescription("Get the screen resolution of the remote machine"),
	), handleScreenSize)
}

// --- Handlers ---

func handleScreenshot(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	monitorID := req.GetFloat("monitor", 0)
	maxWidth := req.GetFloat("max_width", 0)
	quality := req.GetFloat("quality", 80)
	format := req.GetString("format", "png")

	var data []byte
	var err error
	switch format {
	case "jpeg":
		data, err = screen.CaptureJPEG(int(monitorID), int(maxWidth), int(quality))
	default:
		data, err = screen.CapturePNG(int(monitorID), int(maxWidth))
	}
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("screenshot failed: %v", err)), nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewImageContent(base64.StdEncoding.EncodeToString(data), "image/"+format),
		},
	}, nil
}

func handleScreenshotBase64(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	monitorID := req.GetFloat("monitor", 0)
	maxWidth := req.GetFloat("max_width", 1280)
	quality := req.GetFloat("quality", 40)

	data, err := screen.CaptureJPEG(int(monitorID), int(maxWidth), int(quality))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("screenshot failed: %v", err)), nil
	}
	b64 := base64.StdEncoding.EncodeToString(data)
	return mcp.NewToolResultText(fmt.Sprintf("data:image/jpeg;base64,%s", b64)), nil
}

func handleNumMonitors(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	n := screen.NumMonitors()
	return mcp.NewToolResultText(fmt.Sprintf("Active displays: %d", n)), nil
}

func handleMouseMove(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	x, err := req.RequireFloat("x")
	if err != nil {
		return mcp.NewToolResultError("x coordinate required"), nil
	}
	y, err := req.RequireFloat("y")
	if err != nil {
		return mcp.NewToolResultError("y coordinate required"), nil
	}
	input.Move(int(x), int(y))
	return mcp.NewToolResultText(fmt.Sprintf("Mouse moved to (%d, %d)", int(x), int(y))), nil
}

func handleMouseClick(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	x, err := req.RequireFloat("x")
	if err != nil {
		return mcp.NewToolResultError("x coordinate required"), nil
	}
	y, err := req.RequireFloat("y")
	if err != nil {
		return mcp.NewToolResultError("y coordinate required"), nil
	}
	button := req.GetString("button", "left")
	clickType := req.GetString("click_type", "single")

	if clickType == "double" {
		input.DoubleClick(int(x), int(y), button)
		return mcp.NewToolResultText(fmt.Sprintf("Double-click at (%d, %d) with %s button", int(x), int(y), button)), nil
	}
	input.Click(int(x), int(y), button)
	return mcp.NewToolResultText(fmt.Sprintf("Clicked at (%d, %d) with %s button", int(x), int(y), button)), nil
}

func handleMouseScroll(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	x, err := req.RequireFloat("x")
	if err != nil {
		return mcp.NewToolResultError("x coordinate required"), nil
	}
	y, err := req.RequireFloat("y")
	if err != nil {
		return mcp.NewToolResultError("y coordinate required"), nil
	}
	dx := req.GetFloat("delta_x", 0)
	dy := req.GetFloat("delta_y", 0)

	input.Scroll(int(x), int(y), int(dx), int(dy))
	return mcp.NewToolResultText(fmt.Sprintf("Scrolled at (%d, %d) by (%d, %d)", int(x), int(y), int(dx), int(dy))), nil
}

func handleMouseDrag(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	fx, err := req.RequireFloat("from_x")
	if err != nil {
		return mcp.NewToolResultError("from_x required"), nil
	}
	fy, err := req.RequireFloat("from_y")
	if err != nil {
		return mcp.NewToolResultError("from_y required"), nil
	}
	tx, err := req.RequireFloat("to_x")
	if err != nil {
		return mcp.NewToolResultError("to_x required"), nil
	}
	ty, err := req.RequireFloat("to_y")
	if err != nil {
		return mcp.NewToolResultError("to_y required"), nil
	}
	input.Drag(int(fx), int(fy), int(tx), int(ty))
	return mcp.NewToolResultText(fmt.Sprintf("Dragged from (%d, %d) to (%d, %d)", int(fx), int(fy), int(tx), int(ty))), nil
}

func handleKeyboardType(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	text, err := req.RequireString("text")
	if err != nil {
		return mcp.NewToolResultError("text required"), nil
	}
	input.TypeText(text)
	return mcp.NewToolResultText(fmt.Sprintf("Typed %d characters", len(text))), nil
}

func handleKeyboardHotkey(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	keys := req.GetStringSlice("keys", []string{})
	if len(keys) == 0 {
		return mcp.NewToolResultError("at least one key required"), nil
	}
	input.Hotkey(keys)
	return mcp.NewToolResultText(fmt.Sprintf("Pressed: %s", strings.Join(keys, " + "))), nil
}

func handleShell(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	command, err := req.RequireString("command")
	if err != nil {
		return mcp.NewToolResultError("command required"), nil
	}
	timeoutSec := req.GetFloat("timeout", 30)
	timeout := time.Duration(timeoutSec) * time.Second

	result, _ := shell.RunDetected(command, timeout)

	output := fmt.Sprintf("Exit code: %d\n", result.ExitCode)
	if result.Stdout != "" {
		output += fmt.Sprintf("STDOUT:\n%s\n", result.Stdout)
	}
	if result.Stderr != "" {
		output += fmt.Sprintf("STDERR:\n%s\n", result.Stderr)
	}
	return mcp.NewToolResultText(strings.TrimSpace(output)), nil
}

func handleFileRead(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return mcp.NewToolResultError("path required"), nil
	}
	encoding := req.GetString("encoding", "text")

	switch encoding {
	case "base64":
		b64, err := files.ReadBase64(path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("read failed: %v", err)), nil
		}
		return mcp.NewToolResultText(b64), nil
	default:
		text, err := files.ReadText(path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("read failed: %v", err)), nil
		}
		if len(text) > 500000 {
			text = text[:500000] + "\n\n... [truncated at 500KB]"
		}
		return mcp.NewToolResultText(text), nil
	}
}

func handleFileWrite(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return mcp.NewToolResultError("path required"), nil
	}
	content, err := req.RequireString("content")
	if err != nil {
		return mcp.NewToolResultError("content required"), nil
	}
	if err := files.WriteText(path, content); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("write failed: %v", err)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Wrote %d bytes to %s", len(content), path)), nil
}

func handleFileList(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	dir := req.GetString("dir", "C:\\")
	entries, err := files.List(dir)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("list failed: %v", err)), nil
	}
	data, _ := json.MarshalIndent(entries, "", "  ")
	return mcp.NewToolResultText(string(data)), nil
}

func handleProcesses(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	procs, err := system.Processes()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("list processes failed: %v", err)), nil
	}
	if len(procs) > 200 {
		procs = procs[:200]
	}
	data, _ := json.MarshalIndent(procs, "", "  ")
	return mcp.NewToolResultText(string(data)), nil
}

func handleClipboardGet(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	text := system.GetClipboard()
	if text == "" {
		return mcp.NewToolResultText("(clipboard is empty)"), nil
	}
	return mcp.NewToolResultText(text), nil
}

func handleClipboardSet(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	text, err := req.RequireString("text")
	if err != nil {
		return mcp.NewToolResultError("text required"), nil
	}
	system.SetClipboard(text)
	return mcp.NewToolResultText(fmt.Sprintf("Clipboard set (%d characters)", len(text))), nil
}

func handleWindowList(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	windows, err := system.Windows()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("list windows failed: %v", err)), nil
	}
	var visible []system.WindowInfo
	for _, w := range windows {
		if w.Visible {
			visible = append(visible, w)
		}
	}
	if len(visible) > 50 {
		visible = visible[:50]
	}
	data, _ := json.MarshalIndent(visible, "", "  ")
	return mcp.NewToolResultText(string(data)), nil
}

func handleScreenSize(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	n := screen.NumMonitors()
	if n == 0 {
		return mcp.NewToolResultError("no displays detected"), nil
	}
	var info []string
	for i := 0; i < n; i++ {
		img, err := screen.Capture(i)
		if err != nil {
			info = append(info, fmt.Sprintf("Monitor %d: error - %v", i, err))
			continue
		}
		b := img.Bounds()
		info = append(info, fmt.Sprintf("Monitor %d: %dx%d", i, b.Dx(), b.Dy()))
	}
	return mcp.NewToolResultText(strings.Join(info, "\n")), nil
}
