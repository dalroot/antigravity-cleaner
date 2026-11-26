$ErrorActionPreference = "Stop"
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

$GithubBaseUrl = "https://github.com/dalroot/antigravity-cleaner/releases/latest/download"
$ExeName = "antigravity-cleaner-windows-amd64.exe"

$HomePath = if ($IsWindows -or $env:OS -like "*Windows*") { $env:USERPROFILE } else { $env:HOME }
$InstallDir = Join-Path $HomePath ".antigravity\bin"
$TargetFile = Join-Path $InstallDir "antigravity-cleaner.exe"

Write-Host "====================================================" -ForegroundColor Cyan
Write-Host "  Antigravity Cleaner Toolkit Installer (v5.4.10)" -ForegroundColor Yellow
Write-Host "  Universal Support: Auto-detects ALL Antigravity Versions (1.x, 2.x, IDE & CLI)" -ForegroundColor Cyan
Write-Host "====================================================" -ForegroundColor Cyan

# 0. Auto-detect Windows System Proxy if env vars are unset
$ProxyArgs = @{}
if (-not $env:HTTPS_PROXY -and -not $env:HTTP_PROXY) {
    try {
        $inet = Get-ItemProperty "HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings" -ErrorAction SilentlyContinue
        if ($inet -and $inet.ProxyEnable -eq 1 -and $inet.ProxyServer) {
            $srv = ($inet.ProxyServer -split ';')[0]
            if ($srv -notmatch '^https?://') { $srv = "http://$srv" }
            $ProxyArgs["Proxy"] = $srv
            Write-Host "Detected Windows System Proxy: $srv" -ForegroundColor DarkCyan
        }
    } catch {}
}

# 1. Create Directory
if (!(Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
}

# 2. Download Native Go Binary
Write-Host "Downloading Antigravity Native Engine..." -ForegroundColor Yellow
$Downloaded = $false
try {
    $DownloadUrl = "$GithubBaseUrl/$ExeName"
    Invoke-WebRequest -Uri $DownloadUrl -OutFile $TargetFile -UseBasicParsing @ProxyArgs
    if ((Test-Path $TargetFile) -and (Get-Item $TargetFile).Length -gt 100000) {
        Copy-Item -Path $TargetFile -Destination (Join-Path $InstallDir "ag-cleaner.exe") -Force
        Copy-Item -Path $TargetFile -Destination (Join-Path $InstallDir "agc.exe") -Force
        $Downloaded = $true
        Write-Host "Download Complete." -ForegroundColor Green
    } else {
        throw "Downloaded binary is incomplete or invalid."
    }
}
catch {
    Write-Host "Native binary download failed ($($_.Exception.Message)). Falling back to legacy PowerShell engine..." -ForegroundColor Gray
    try {
        $LegacyScript = Join-Path $InstallDir "Antigravity.ps1"
        Invoke-WebRequest -Uri "https://raw.githubusercontent.com/dalroot/antigravity-cleaner/main/legacy/Antigravity.ps1" -OutFile $LegacyScript -UseBasicParsing @ProxyArgs
        Start-Process powershell.exe -ArgumentList "-NoProfile -ExecutionPolicy Bypass -File `"$LegacyScript`""
    } catch {
        Write-Host "Fallback download also failed: $($_.Exception.Message)" -ForegroundColor Red
    }
}

# 3. Add to User PATH cleanly (always ensure PATH is registered even if binary is added later)
$UserPath = [Environment]::GetEnvironmentVariable("PATH", "User")
$PathEntries = @()
if ($UserPath) {
    $PathEntries = $UserPath -split ';' | Where-Object { $_ -and $_.Trim() -ne "" }
}
if ($PathEntries -notcontains $InstallDir) {
    $PathEntries += $InstallDir
    [Environment]::SetEnvironmentVariable("PATH", ($PathEntries -join ';'), "User")
    Write-Host "Added $InstallDir to user PATH." -ForegroundColor Cyan
}
if (($env:PATH -split ';') -notcontains $InstallDir) {
    $env:PATH = ($env:PATH.TrimEnd(';') + ";" + $InstallDir)
}

if (-not $Downloaded) {
    return
}

# 4. Create Desktop Shortcut
try {
    $WshShell = New-Object -ComObject WScript.Shell
    $DesktopPath = [Environment]::GetFolderPath("Desktop")
    $ShortcutFile = "$DesktopPath\Antigravity Cleaner.lnk"
    $Shortcut = $WshShell.CreateShortcut($ShortcutFile)
    $Shortcut.TargetPath = $TargetFile
    $Shortcut.Save()
    Write-Host "Desktop shortcut created successfully!" -ForegroundColor Green
}
catch {
    Write-Host "Could not create desktop shortcut automatically." -ForegroundColor Gray
}

Write-Host ""
Write-Host "Installation Complete! Launching Antigravity Cleaner..." -ForegroundColor Green
Write-Host "If this tool helped you, please star our repository on GitHub:" -ForegroundColor Yellow
Write-Host "   https://github.com/dalroot/antigravity-cleaner" -ForegroundColor Cyan
Write-Host ""
Start-Process $TargetFile
