package files

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Entry struct {
	Name    string `json:"name"`
	IsDir   bool   `json:"is_dir"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
}

func List(dir string) ([]Entry, error) {
	if dir == "" {
		dir = "C:\\"
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}
	result := make([]Entry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		size := int64(0)
		modTime := ""
		if err == nil {
			size = info.Size()
			modTime = info.ModTime().Format(time.RFC3339)
		}
		result = append(result, Entry{
			Name:    e.Name(),
			IsDir:   e.IsDir(),
			Size:    size,
			ModTime: modTime,
		})
	}
	return result, nil
}

func Read(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", path, err)
	}
	return data, nil
}

func ReadText(path string) (string, error) {
	data, err := Read(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func ReadBase64(path string) (string, error) {
	data, err := Read(path)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

func Write(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, content, 0644); err != nil {
		return fmt.Errorf("write file %s: %w", path, err)
	}
	return nil
}

func WriteText(path, text string) error {
	return Write(path, []byte(text))
}

func Delete(path string) error {
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("delete %s: %w", path, err)
	}
	return nil
}

func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func IsDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func Size(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("stat %s: %w", path, err)
	}
	return info.Size(), nil
}

func SanitizePath(path string) string {
	path = strings.TrimSpace(path)
	if len(path) >= 2 && path[1] == ':' && path[0] >= 'A' && path[0] <= 'Z' {
		return path
	}
	if !strings.HasPrefix(path, "\\") && !strings.HasPrefix(path, "/") && !strings.Contains(path, ":") {
		path = filepath.Join("C:\\", path)
	}
	return filepath.Clean(path)
}
