# Daljinac2 V2 — MCP User Manual

**Version:** `2.0.0-dev.20260718.2`
**MCP Server:** `Daljinac2 Remote Agent`
**Port:** `1984`
**Auth:** Bearer token

---

## Connection

### Via SSH tunnel
```
Endpoint: http://127.0.0.1:7182/mcp
```
SSH reverse tunnel preko VPS (31.220.74.109). Tunnel se održava automatski.

### Via Tailscale
```
Endpoint: http://100.126.144.88:1984/mcp
```
Direktna konekcija preko Tailscale mreže.

### Auth header
```
Authorization: Bearer 234d130007706cd69359c94b89d3dd70
```

---

## MCP Protocol Basics

Daljinac2 v2 koristi **MCP Streamable HTTP** transport.

**Obavezan handshake:**
1. Pošalji `initialize` → server vrati `Mcp-Session-Id` header
2. Sve naredne requeste šalji sa headerom `Mcp-Session-Id: <session_id>`

**Primjer (curl):**
```bash
# Step 1: Initialize
INIT=$(curl -s -D - http://100.126.144.88:1984/mcp \
  -H "Authorization: Bearer 234d130007706cd69359c94b89d3dd70" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"my-client","version":"1.0"}}}')
SESSION=$(echo "$INIT" | grep -i "^mcp-session-id:" | awk '{print $2}')

# Step 2: Use session
curl -s http://100.126.144.88:1984/mcp \
  -H "Authorization: Bearer 234d130007706cd69359c94b89d3dd70" \
  -H "Content-Type: application/json" \
  -H "Mcp-Session-Id: $SESSION" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
```

Session ID je format `mcp-session-<uuid>`. Server je stateless (ne pamti session), ali validira format session ID.

---

## Tool Reference

### Screen & Display

#### `get_screen_size`
Vraća rezoluciju svakog monitora.
- **Args:** none
- **Output:** `"Monitor 0: 3840x2160"`

#### `num_monitors`
Broj aktivnih monitora.
- **Args:** none
- **Output:** `"Active displays: 1"`

#### `screenshot`
Captures screenshot kao base64 sliku.
- **Args:**
  - `monitor` (number, opc, default: 0) — indeks monitora
  - `max_width` (number, opc, default: 0) — resize na max piksela (0 = full rezolucija)
  - `quality` (number, opc, default: 80) — JPEG quality 1-100 (samo za JPEG)
  - `format` (string, opc, "png" | "jpeg", default: "png")
- **Output:** image content (base64 encoded)
- **Ponašanje:** Vraća base64-decoded image data. Za PNG, 4K ekran ~1.2MB. Za JPEG, ~300KB. Sa resize 800px, ~200KB.

#### `screenshot_base64`
Captures screenshot kao data URI string.
- **Args:**
  - `monitor` (number, opc, default: 0)
  - `max_width` (number, opc, default: 1280)
  - `quality` (number, opc, default: 40)
- **Output:** `"data:image/jpeg;base64,..."`
- **Namjena:** Za inline prikaz. Uvijek JPEG. Default 1280px quality 40 daje ~20-70KB string.

---

### Mouse Input

#### `mouse_move`
Pomjeri kursor na apsolutne koordinate.
- **Args:**
  - `x` (number, req) — X koordinata
  - `y` (number, req) — Y koordinata
- **Output:** `"Mouse moved to (1920, 1080)"`

#### `mouse_click`
Klik na poziciji.
- **Args:**
  - `x` (number, req)
  - `y` (number, req)
  - `button` (string, opc, "left" | "right" | "middle", default: "left")
  - `click_type` (string, opc, "single" | "double", default: "single")
- **Output:** `"Clicked at (1920, 1080) with left button"`

#### `mouse_scroll`
Scroll na poziciji.
- **Args:**
  - `x` (number, req)
  - `y` (number, req)
  - `delta_x` (number, opc, default: 0) — pozitivno = desno, negativno = lijevo
  - `delta_y` (number, opc, default: 0) — pozitivno = dole, negativno = gore
- **Output:** `"Scrolled at (100, 100) by (0, 3)"`

#### `mouse_drag`
Klikni i prevuci.
- **Args:**
  - `from_x` (number, req)
  - `from_y` (number, req)
  - `to_x` (number, req)
  - `to_y` (number, req)
- **Output:** `"Dragged from (100, 100) to (300, 300)"`

---

### Keyboard Input

#### `keyboard_type`
Ukucaj tekst (Unicode).
- **Args:**
  - `text` (string, req) — tekst za kucanje
- **Output:** `"Typed 15 characters"`
- **Napomena:** Podržava Unicode karaktere. Ne radi sa modifikatorima (Ctrl, Alt).

#### `keyboard_hotkey`
Pritisni kombinaciju tastera.
- **Args:**
  - `keys` (array of strings, req) — niz key names
- **Output:** `"Pressed: win + r"`
- **Podržani key names:** `ctrl`, `alt`, `shift`, `win`, `a`-`z`, `0`-`9`, `f1`-`f12`, `enter`, `tab`, `escape`, `backspace`, `delete`, `space`, `up`, `down`, `left`, `right`, `home`, `end`, `pageup`, `pagedown`, `capslock`, `insert`, `printscreen`, `scrolllock`, `pause`, `numlock`, `apps`
- **Primjeri:**
  - `["win","r"]` → Run dialog
  - `["win","d"]` → Show desktop
  - `["alt","f4"]` → Close window
  - `["ctrl","c"]` → Copy
  - `["ctrl","v"]` → Paste
  - `["ctrl","shift","escape"]` → Task Manager
  - `["alt","tab"]` → Switch window

---

### Shell Execution

#### `shell`
Izvrši komandu na remote Windows mašini.
- **Args:**
  - `command` (string, req) — komanda za izvršenje
  - `timeout` (number, opc, default: 30, max: 300) — timeout u sekundama
- **Output format:**
  ```
  Exit code: 0
  STDOUT:
  <stdout output>
  STDERR:
  <stderr output>
  ```
- **Auto-detection:** Ako komanda počinje sa "powershell" ili "ps", izvršava se kroz PowerShell. Inače kroz CMD.
- **CLIXML filtering:** PowerShell izlaz se automatski filtrira od CLIXML markup-a (samo čist tekst ostaje).
- **Timeout:** Ako komanda pređe timeout, terminira se. Default 30s, max 300s (5 minuta).

**Primjeri:**
```
command: "hostname"
command: "powershell Get-Process | Select-Object Name,CPU | Format-Table"
command: "dir C:\\"
command: "ping -n 5 8.8.8.8"
command: "powershell Get-Service | Where-Object Status -eq Running | Format-Table Name"
```

---

### File Operations

#### `file_list`
Listing direktorijuma.
- **Args:**
  - `dir` (string, opc, default: `C:\`) — putanja do direktorijuma
- **Output:** JSON array:
  ```json
  [
    {"name": "Program Files", "is_dir": true, "size": 0, "mod_time": "2026-06-30T13:05:55+08:00"},
    {"name": "test.txt", "is_dir": false, "size": 1024, "mod_time": "2026-07-18T14:00:00+08:00"}
  ]
  ```
- **Napomena:** Putanje moraju koristiti `\\` (backslash escape) ili `/` (forward slash).

#### `file_read`
Čitanje fajla.
- **Args:**
  - `path` (string, req) — apsolutna putanja
  - `encoding` (string, opc, "text" | "base64", default: "text")
- **Output:** Sadržaj fajla
- **Limit:** 500KB za text encoding (truncated sa notom). Do 10MB za base64.
- **Primjeri:**
  - `file_read` sa `encoding: "text"` → čita kao string
  - `file_read` sa `encoding: "base64"` → vraća base64 string (za binarne fajlove)

#### `file_write`
Pisanje fajla.
- **Args:**
  - `path` (string, req) — apsolutna putanja
  - `content` (string, req) — sadržaj
- **Output:** `"Wrote 38 bytes to C:\path\file.txt"`
- **Ponašanje:** Overwrite-a postojeći fajl. Ne podržava append.

---

### System Tools

#### `processes`
Lista pokrenutih procesa.
- **Args:** none
- **Output:** JSON array:
  ```json
  [
    {"pid": 4, "name": "System", "cpu_percent": 1.35, "memory_bytes": 7327744, "create_time": 1784351097980}
  ]
  ```
- **Limit:** Maks 200 procesa (najrelevantniji).

#### `clipboard_get`
Čita tekst sa clipboard-a.
- **Args:** none
- **Output:** Tekst sa clipboard-a, ili `"(clipboard is empty)"`

#### `clipboard_set`
Piše tekst na clipboard.
- **Args:**
  - `text` (string, req) — tekst za postaviti
- **Output:** `"Clipboard set (37 characters)"`

#### `window_list`
Lista otvorenih prozora.
- **Args:** none
- **Output:** JSON array:
  ```json
  [
    {"title": "Program Manager", "class_name": "Progman", "pid": 7356, "visible": true, "x": 0, "y": 672, "width": 1280, "height": 48}
  ]
  ```
- **Filter:** Samo visible prozori (nevidljivi se preskaču). Limit 50 prozora.
- **Napomena:** `class_name` može imati trailing null karaktere.

---

## Common Recipes

### "Šta je na ekranu?"
```
1. get_screen_size → saznaj rezoluciju
2. screenshot (JPEG, max_width=1280) → vidi šta se dešava
```

### "Pokreni program"
```
1. keyboard_hotkey: ["win","r"] → Run dialog
2. keyboard_type: "notepad" → ukucaj ime
3. keyboard_hotkey: ["enter"] → pokreni
```

### "Uploadaj fajl na remote mašinu"
```
1. file_write: {"path": "C:\\temp\\file.txt", "content": "..."} 
```

### "Preuzmi fajl sa remote mašine"
```
1. file_read: {"path": "C:\\temp\\file.txt", "encoding": "base64"}
2. Dekodiraj base64 na klijentu
```

### "Pokreni PowerShell skriptu"
```
1. shell: {"command": "powershell ./script.ps1", "timeout": 60}
```

### "Instaliraj softver"
```
1. keyboard_hotkey: ["win","r"]
2. keyboard_type: "cmd"
3. keyboard_hotkey: ["ctrl","shift","enter"] → admin CMD
4. keyboard_hotkey: ["alt","y"] → potvrdi UAC
5. keyboard_type: "winget install <package>"
6. keyboard_hotkey: ["enter"]
```

### "Kopiraj tekst sa ekrana (OCR workflow)"
```
1. screenshot_base64 → dobij sliku
2. Klijent OCR-uje sliku
```

### "Kill proces"
```
1. processes → nađi PID
2. shell: {"command": "taskkill /PID 1234 /F"}
```

---

## Implementacioni Detalji

- **CLIXML filter:** PowerShell `Format-Table` i slične komande proizvode CLIXML markup u stderr. Daljinac2 automatski filtrira CLIXML (regex: `#< CLIXML\r\n` i XML tagovi `<S>`, `<SR>`, `<T>`, itd.) tako da u stdout dolazi samo čist tekst.
- **Screenshot engine:** Koristi `github.com/kbinani/screenshot` za capture. JPEG enkodiranje preko Go standard library.
- **Keyboard/Mouse:** Win32 `SendInput` API.
- **Clipboard:** Win32 `OpenClipboard`/`GetClipboardData`/`SetClipboardData`.
- **Shell execution:** `exec.Command` sa detekcijom (CMD vs PowerShell) i timeout control.
- **File tools:** Standard Go file I/O. Max 500KB za text read, bez limita za base64 read.
- **Process listing:** `CreateToolhelp32Snapshot` Win32 API.
- **Window listing:** `EnumWindows` Win32 API.
- **Auth:** Middleware koji provjerava Bearer token prije svakog handlera.

---

## Troubleshooting

### "Invalid session ID"
Security: Potrebno je prvo pozvati `initialize` da dobiješ session ID. Ne zaboravi `Mcp-Session-Id` header.

### "Request body is not valid json"
Security: Content-Type mora biti `application/json`.

### Komanda ne vraća izlaz
Provjeri:
- Da li komanda postoji na remote mašini (`wmic` nije dostupan na novijim Windowsima)
- Da li koristiš PowerShell sintaksu ispravno
- Da li timeout nije prekratak

### Screenshot vraća praznu sliku
Provjeri monitor index (`get_screen_size` prvo). Ako je Beelink, monitor 0 je 3840x2160.

### File path ne radi
Koristi `C:\\path\\file.txt` (escape backslasha) ili `C:/path/file.txt` (forward slash radi).
