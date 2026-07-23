package webcam

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/image/draw"
)

type DeviceInfo struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
}

var psCapture = func(outPath, cameraIdx string) error {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive",
		"-Command", fmt.Sprintf(`
try {
    $dm = New-Object -ComObject WIA.DeviceManager
    $di = $null
    $idx = 0
    foreach ($d in $dm.DeviceInfos) {
        if ($d.Type -eq 1) {
            if ($idx -eq %s) { $di = $d; break }
            $idx++
        }
    }
    if (-not $di) { Write-Error "no camera at index %s"; exit 1 }
    $dev = $di.Connect()
    $pic = $dev.ExecuteCommand("{AF933CAC-ACAD-11D2-A093-00C04F72DC3C}")
    $img = $pic.Transfer()
    $img.SaveFile("%s")
    Write-Output "ok"
} catch {
    Write-Error $_.Exception.Message
    exit 1
}
`, cameraIdx, cameraIdx, strings.ReplaceAll(outPath, "'", "''")))

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("powershell: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

var psListDevices = func() ([]byte, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive",
		"-Command", `
$dm = New-Object -ComObject WIA.DeviceManager
$i = 0
foreach ($d in $dm.DeviceInfos) {
    if ($d.Type -eq 1) {
        Write-Output "$i|$($d.Properties('Name').Value)"
        $i++
    }
}
`)
	return cmd.Output()
}

func ListDevices() ([]DeviceInfo, error) {
	out, err := psListDevices()
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var devices []DeviceInfo
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		var idx int
		fmt.Sscanf(parts[0], "%d", &idx)
		devices = append(devices, DeviceInfo{
			Index: idx,
			Name:  strings.TrimSpace(parts[1]),
		})
	}
	if devices == nil {
		devices = []DeviceInfo{}
	}
	return devices, nil
}

func CaptureJPEG(cameraIndex int, maxWidth int, quality int) ([]byte, error) {
	tmpDir := os.TempDir()
	outPath := filepath.Join(tmpDir, fmt.Sprintf("agent-webcam-%d.jpg", time.Now().UnixNano()))
	defer os.Remove(outPath)

	if err := psCapture(outPath, fmt.Sprintf("%d", cameraIndex)); err != nil {
		return nil, fmt.Errorf("webcam capture: %w", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		return nil, fmt.Errorf("read webcam image: %w", err)
	}

	if maxWidth > 0 {
		data, err = resizeJPEG(data, maxWidth, quality)
		if err != nil {
			return nil, err
		}
	} else if quality > 0 && quality < 100 {
		data, err = reencodeJPEG(data, quality)
		if err != nil {
			return nil, err
		}
	}

	return data, nil
}

func CaptureJPEGBase64(cameraIndex int, maxWidth int, quality int) (string, error) {
	data, err := CaptureJPEG(cameraIndex, maxWidth, quality)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("data:image/jpeg;base64,%s", base64.StdEncoding.EncodeToString(data)), nil
}

func resizeJPEG(data []byte, maxWidth int, quality int) ([]byte, error) {
	src, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode for resize: %w", err)
	}

	b := src.Bounds()
	w := b.Dx()
	h := b.Dy()
	if w <= maxWidth {
		return reencodeJPEG(data, quality)
	}

	ratio := float64(maxWidth) / float64(w)
	newW := maxWidth
	newH := int(float64(h) * ratio)

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)

	var buf bytes.Buffer
	err = jpeg.Encode(&buf, dst, &jpeg.Options{Quality: quality})
	if err != nil {
		return nil, fmt.Errorf("encode resized: %w", err)
	}
	return buf.Bytes(), nil
}

func reencodeJPEG(data []byte, quality int) ([]byte, error) {
	src, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode for reencode: %w", err)
	}
	rgba := image.NewRGBA(src.Bounds())
	draw.Draw(rgba, rgba.Bounds(), src, src.Bounds().Min, draw.Over)
	var buf bytes.Buffer
	err = jpeg.Encode(&buf, rgba, &jpeg.Options{Quality: quality})
	if err != nil {
		return nil, fmt.Errorf("encode reencoded: %w", err)
	}
	return buf.Bytes(), nil
}


