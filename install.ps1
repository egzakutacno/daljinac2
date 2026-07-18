param([switch]$notray)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"
$VerbosePreference = "Continue"

$Dir = "C:\daljinac2"
$Exe = "$Dir\daljinac2.exe"
$URL = "https://github.com/egzakutacno/daljinac2/releases/latest/download/daljinac2.exe"
$ExtraArgs = if ($notray) { "-notray" } else { "" }

try {
    # Kill any running instance FIRST
    Write-Host "[0/3] Killing old process..."
    Get-Process daljinac2 -ErrorAction SilentlyContinue | Stop-Process -Force
    Start-Sleep -Seconds 2

    # Make sure the dir exists
    mkdir $Dir -Force | Out-Null

    Write-Host "[1/3] Downloading..."
    Invoke-WebRequest $URL -OutFile "$Exe.new" -UseBasicParsing
    Write-Host "       $((Get-Item "$Exe.new").Length) bytes"

    # Wait for file locks to release
    Start-Sleep -Seconds 1

    Write-Host "[1b/3] Replacing old binary..."
    Get-Process daljinac2 -ErrorAction SilentlyContinue | Stop-Process -Force
    Start-Sleep -Seconds 1
    Move-Item -Force "$Exe.new" $Exe

    Write-Host "[2/3] Installing scheduled task..."
    Remove-Item C:\daljinac2\watchdog.vbs -Force -ErrorAction SilentlyContinue
    schtasks /delete /tn Daljinac2 /f 2>$null
    schtasks /delete /tn Daljinac2Watch /f 2>$null

    Write-Host "       Creating Daljinac2 ONLOGON task..."
    $taskCmd = "$Exe"
    if ($ExtraArgs) { $taskCmd = "$Exe $ExtraArgs" }
    schtasks /create /tn Daljinac2 /tr "$taskCmd" /sc ONLOGON /it /rl HIGHEST /f
    if ($LASTEXITCODE -ne 0) { throw "schtasks Daljinac2 failed (exit=$LASTEXITCODE)" }

    Write-Host "       Creating Daljinac2Watch (5min watchdog)..."
    $vbsContent = 'CreateObject("WScript.Shell").Run "schtasks /run /tn Daljinac2", 0, False'
    Set-Content -Path C:\daljinac2\watchdog.vbs -Value $vbsContent -Encoding ASCII
    schtasks /create /tn Daljinac2Watch /tr "wscript.exe //B C:\daljinac2\watchdog.vbs" /sc MINUTE /mo 5 /f
    if ($LASTEXITCODE -ne 0) { throw "schtasks Daljinac2Watch failed (exit=$LASTEXITCODE)" }

    # Add Run registry key as fallback
    try {
        $reg = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey("Software\Microsoft\Windows\CurrentVersion\Run", $true)
        if ($reg) {
            $reg.SetValue("Daljinac2", "`"$Exe`"")
            $reg.Close()
            Write-Host "       Run registry key added (fallback)"
        }
    } catch {
        Write-Host "       (Run registry key skipped: $($_.Exception.Message))"
    }

    Write-Host "[3/3] Starting..."
    $cmd = if ($ExtraArgs) { "$Exe $ExtraArgs" } else { $Exe }
    ([wmiclass]'Win32_Process').Create($cmd) | Out-Null

    Write-Host ""
    Write-Host "DONE." -ForegroundColor Green
    if ($notray) { Write-Host "  Mode: no-tray (background)" }
} catch {
    Write-Host ""
    Write-Host "ERROR: $_" -ForegroundColor Red
    exit 1
}
