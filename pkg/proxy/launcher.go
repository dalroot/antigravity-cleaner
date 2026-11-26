package proxy

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/dalroot/antigravity-cleaner/pkg/account"
)

// AppTarget describes an installed Antigravity application target (Standard 2.0 or IDE)
type AppTarget struct {
	Name    string // "Antigravity" or "Antigravity IDE"
	ExePath string
	IsIDE   bool
}

// FindAllAntigravityExecutables finds all distinct Antigravity GUI application targets installed
func FindAllAntigravityExecutables() []AppTarget {
	home, _ := os.UserHomeDir()
	var targets []AppTarget
	seen := make(map[string]bool)

	add := func(name, path string, isIDE bool) {
		if path == "" {
			return
		}
		if real, err := filepath.EvalSymlinks(path); err == nil && real != "" {
			path = real
		}
		clean := filepath.Clean(path)
		if !seen[clean] {
			if fi, err := os.Stat(clean); err == nil && !fi.IsDir() {
				seen[clean] = true
				targets = append(targets, AppTarget{
					Name:    name,
					ExePath: clean,
					IsIDE:   isIDE,
				})
			}
		}
	}

	// 1. Windows direct program checks
	if runtime.GOOS == "windows" {
		localAppData := os.Getenv("LOCALAPPDATA")
		progFiles := os.Getenv("PROGRAMFILES")
		progFilesX86 := os.Getenv("ProgramFiles(x86)")

		// Antigravity 2.0 (Official app)
		add("Antigravity", filepath.Join(localAppData, "Programs", "antigravity", "Antigravity.exe"), false)
		add("Antigravity", filepath.Join(localAppData, "Programs", "Antigravity", "Antigravity.exe"), false)
		add("Antigravity", filepath.Join(progFiles, "Antigravity", "Antigravity.exe"), false)
		add("Antigravity", filepath.Join(progFilesX86, "Antigravity", "Antigravity.exe"), false)

		// Antigravity IDE (VS Code based)
		add("Antigravity IDE", filepath.Join(localAppData, "Programs", "Antigravity IDE", "Antigravity IDE.exe"), true)
		add("Antigravity IDE", filepath.Join(progFiles, "Antigravity IDE", "Antigravity IDE.exe"), true)

		// Scoop installations
		userProfile := os.Getenv("USERPROFILE")
		if userProfile != "" {
			add("Antigravity", filepath.Join(userProfile, "scoop", "apps", "antigravity", "current", "Antigravity.exe"), false)
			add("Antigravity IDE", filepath.Join(userProfile, "scoop", "apps", "antigravity-ide", "current", "Antigravity IDE.exe"), true)
		}
	}

	// 2. Linux checks
	if runtime.GOOS == "linux" {
		// Antigravity 2.0
		add("Antigravity", filepath.Join(home, "Applications", "Antigravity", "antigravity"), false)
		add("Antigravity", filepath.Join(home, "Applications", "antigravity", "antigravity"), false)
		add("Antigravity", filepath.Join(home, "Applications", "Antigravity-x64", "antigravity"), false)
		add("Antigravity", "/usr/share/antigravity/bin/antigravity", false)
		add("Antigravity", "/usr/share/antigravity/antigravity", false)
		add("Antigravity", "/opt/antigravity/antigravity", false)
		add("Antigravity", "/opt/Antigravity/antigravity", false)
		add("Antigravity", "/usr/local/bin/antigravity", false)
		add("Antigravity", "/usr/bin/antigravity", false)

		// Antigravity IDE
		add("Antigravity IDE", "/usr/share/antigravity-ide/antigravity", true)
		add("Antigravity IDE", "/opt/antigravity-ide/antigravity", true)
		add("Antigravity IDE", "/opt/Antigravity IDE/antigravity", true)
	}

	// 3. macOS checks
	if runtime.GOOS == "darwin" {
		add("Antigravity", "/Applications/Antigravity.app/Contents/MacOS/Antigravity", false)
		add("Antigravity", "/Applications/Antigravity.app/Contents/MacOS/Electron", false)
		add("Antigravity IDE", "/Applications/Antigravity IDE.app/Contents/MacOS/Electron", true)
	}

	// 4. Dynamic PATH discovery
	for _, name := range []string{"antigravity", "antigravity.exe"} {
		if path, err := exec.LookPath(name); err == nil && path != "" {
			add("Antigravity", path, false)
		}
	}
	if path, err := exec.LookPath("antigravity-ide"); err == nil && path != "" {
		add("Antigravity IDE", path, true)
	}

	// Sort targets so Antigravity IDE appears first, followed by standalone Antigravity
	var ideTargets []AppTarget
	var standardTargets []AppTarget
	for _, t := range targets {
		if t.IsIDE {
			ideTargets = append(ideTargets, t)
		} else {
			standardTargets = append(standardTargets, t)
		}
	}
	return append(ideTargets, standardTargets...)
}

// FindTargetExecutable finds an Antigravity executable matching preference ("ide", "app"/"standalone", or empty for default)
func FindTargetExecutable(preference string) string {
	targets := FindAllAntigravityExecutables()
	if len(targets) == 0 {
		return ""
	}
	prefLower := strings.ToLower(strings.TrimSpace(preference))
	if prefLower == "ide" {
		for _, t := range targets {
			if t.IsIDE {
				return t.ExePath
			}
		}
	} else if prefLower == "app" || prefLower == "standalone" {
		for _, t := range targets {
			if !t.IsIDE {
				return t.ExePath
			}
		}
	}
	// Default: Return the first target (IDE takes precedence if installed, else standalone)
	return targets[0].ExePath
}

// FindAntigravityExecutable finds the primary Antigravity GUI application
func FindAntigravityExecutable() string {
	return FindTargetExecutable("")
}

// BuildProxyEnv constructs clean proxy environment variables for child processes
func BuildProxyEnv(proxyURL string) []string {
	env := os.Environ()

	var httpProxy string
	var socksProxy string

	hostPort := CleanHostPort(proxyURL)
	var host string
	var port int
	if h, pStr, err := net.SplitHostPort(hostPort); err == nil {
		host = h
		if p, err := net.LookupPort("tcp", pStr); err == nil {
			port = p
		}
	} else {
		host = "127.0.0.1"
		port = 10808
	}

	// Active socket handshake verification
	ep, probed := ProbeProxyPort(host, port, 400*time.Millisecond)

	isSocks := strings.HasPrefix(proxyURL, "socks")
	if probed && (ep.HasHttp || ep.Type == "mixed" || ep.Type == "http") {
		httpProxy = fmt.Sprintf("http://%s:%d", host, port)
		if ep.HasSocks {
			socksProxy = fmt.Sprintf("socks5h://%s:%d", host, port)
		} else {
			socksProxy = httpProxy
		}
	} else if isSocks {
		socksProxy = proxyURL
		if !strings.HasPrefix(proxyURL, "socks5h://") && strings.HasPrefix(proxyURL, "socks5://") {
			socksProxy = strings.Replace(proxyURL, "socks5://", "socks5h://", 1)
		}
	} else {
		httpProxy = proxyURL
		socksProxy = proxyURL
	}

	// Preserve user existing NO_PROXY and ensure essential loopbacks are covered
	existingNoProxy := os.Getenv("NO_PROXY")
	if existingNoProxy == "" {
		existingNoProxy = os.Getenv("no_proxy")
	}
	mergedNoProxy := "localhost,127.0.0.1,::1"
	if existingNoProxy != "" {
		mergedNoProxy = existingNoProxy
		for _, loop := range []string{"localhost", "127.0.0.1", "::1"} {
			if !strings.Contains(mergedNoProxy, loop) {
				mergedNoProxy += "," + loop
			}
		}
	}

	overrides := map[string]string{
		"ALL_PROXY": socksProxy,
		"all_proxy": socksProxy,
		"NO_PROXY":  mergedNoProxy,
		"no_proxy":  mergedNoProxy,
	}

	if httpProxy != "" {
		overrides["HTTPS_PROXY"] = httpProxy
		overrides["https_proxy"] = httpProxy
		overrides["HTTP_PROXY"] = httpProxy
		overrides["http_proxy"] = httpProxy
		overrides["GRPC_PROXY"] = httpProxy
	}

	// Filter existing proxy envs and sensitive leak variables
	leakKeys := map[string]bool{
		"JETSKI_OAUTH_TOKEN":             true,
		"JETSKI_TEST_GAIA_TOKEN":        true,
		"GEMINI_API_KEY":                true,
		"GOOGLE_API_KEY":                true,
		"GOOGLE_APPLICATION_CREDENTIALS": true,
	}

	// Proxy keys to remove if we are overriding or if using socks (to prevent inherited socks5h from breaking Node.js)
	stripProxyKeys := map[string]bool{
		"ALL_PROXY":   true,
		"all_proxy":   true,
		"HTTPS_PROXY": true,
		"https_proxy": true,
		"HTTP_PROXY":  true,
		"http_proxy":  true,
		"GRPC_PROXY":  true,
	}

	var cleanEnv []string
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) > 0 {
			key := parts[0]
			keyUpper := strings.ToUpper(key)
			if stripProxyKeys[key] || stripProxyKeys[keyUpper] {
				continue
			}
			if leakKeys[key] || leakKeys[keyUpper] {
				continue
			}
		}
		cleanEnv = append(cleanEnv, e)
	}

	for k, v := range overrides {
		cleanEnv = append(cleanEnv, fmt.Sprintf("%s=%s", k, v))
	}

	return cleanEnv
}

// CleanChromiumProxyURL normalizes proxy schemes for Chromium/Electron.
// Chromium does NOT support socks5h:// (it causes ERR_NO_SUPPORTED_PROXIES).
// Chromium's socks5:// implementation automatically performs DNS resolution on the proxy.
func CleanChromiumProxyURL(proxyURL string) string {
	return strings.Replace(proxyURL, "socks5h://", "socks5://", 1)
}

// LaunchAntigravityWithProxy launches Antigravity with injected proxy variables (No TUN needed)
func LaunchAntigravityWithProxy(appPath string, proxyURL string, extraArgs []string) error {
	if appPath == "" {
		appPath = FindAntigravityExecutable()
		if appPath == "" {
			return fmt.Errorf("Antigravity executable not found")
		}
	}

	if proxyURL == "" {
		best, err := GetBestProxy()
		if err != nil {
			return err
		}
		proxyURL = best.URL
	}

	chromiumProxy := CleanChromiumProxyURL(proxyURL)

	// Build arguments: pass --proxy-server and loopback filter to Chromium/Electron without quotes
	args := []string{
		fmt.Sprintf("--proxy-server=%s", chromiumProxy),
		"--proxy-bypass-list=localhost;127.0.0.1;::1",
	}
	args = append(args, extraArgs...)

	cmd := exec.Command(appPath, args...)
	cmd.Dir = filepath.Dir(appPath)
	cmd.Env = BuildProxyEnv(proxyURL)
	// Detach stdio pipes to prevent broken-pipe write EIO crashes when parent terminal closes
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil

	// Ensure bi-directional token bridge is synchronized before launching
	_ = account.SyncTokenBridge(nil)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start Antigravity with proxy: %w", err)
	}

	return nil
}

// GenerateNoTunDesktopLauncher creates .desktop shortcuts for both Antigravity and Antigravity Cleaner GUI
func GenerateNoTunDesktopLauncher(proxyURL string) (string, error) {
	appPath := FindAntigravityExecutable()
	if appPath == "" {
		return "", fmt.Errorf("Antigravity executable not found")
	}

	// Resolve symlinks on appPath before extracting icons/dir
	if realPath, err := filepath.EvalSymlinks(appPath); err == nil && realPath != "" {
		appPath = realPath
	}

	if proxyURL == "" {
		best, err := GetBestProxy()
		if err != nil {
			return "", err
		}
		proxyURL = best.URL
	}

	home, _ := os.UserHomeDir()

	// Ensure bi-directional token bridge is synchronized
	_ = account.SyncTokenBridge(nil)

	if runtime.GOOS == "linux" {
		binDir := filepath.Join(home, ".local", "bin")
		_ = os.MkdirAll(binDir, 0755)

		desktopAppDir := filepath.Join(home, ".local", "share", "applications")
		_ = os.MkdirAll(desktopAppDir, 0755)

		appDir := filepath.Dir(appPath)
		workDir := appDir
		if strings.EqualFold(filepath.Base(appDir), "bin") {
			workDir = filepath.Dir(appDir)
		}

		// 1. Create Smart Launcher script in ~/.local/bin/antigravity-smart-launcher.sh
		smartLauncherScript := filepath.Join(binDir, "antigravity-smart-launcher.sh")
		scriptContent := fmt.Sprintf(`#!/usr/bin/env bash
# Smart Proxy Launcher for Google Antigravity
# Auto-detects running proxy (v2ray, NekoBox, Clash, Hiddify) and launches Antigravity

APP_PATH="%s"

# 1. Clean stale singleton locks from ungraceful exits or display disconnects
LOCK_FILE="$HOME/.config/Antigravity/SingletonLock"
if [ -L "$LOCK_FILE" ]; then
    LOCK_TARGET=$(readlink "$LOCK_FILE" 2>/dev/null)
    LOCK_PID="${LOCK_TARGET##*-}"
    if [ -n "$LOCK_PID" ] && ! kill -0 "$LOCK_PID" 2>/dev/null; then
        rm -f "$LOCK_FILE" "$HOME/.config/Antigravity/SingletonSocket" "$HOME/.config/Antigravity/SingletonCookie" 2>/dev/null
    fi
fi

# 2. Dynamic proxy port detection with active protocol handshake
CANDIDATES=("10808" "7890" "2080" "10809" "2081" "1080")
ACTIVE_PORT=""
IS_HTTP="0"

for port in "${CANDIDATES[@]}"; do
    if exec 3<>/dev/tcp/127.0.0.1/$port 2>/dev/null; then
        ACTIVE_PORT="$port"
        printf "CONNECT cloudcode-pa.googleapis.com:443 HTTP/1.1\r\nHost: cloudcode-pa.googleapis.com:443\r\n\r\n" >&3
        read -t 0.3 -u 3 resp || true
        exec 3>&- 2>/dev/null || true
        if [[ "$resp" == HTTP/*200* ]] || [[ "$resp" == HTTP/* ]]; then
            IS_HTTP="1"
        fi
        break
    fi
done

# 3. Resilient fallback: Never abort or lock out the user!
if [ -z "$ACTIVE_PORT" ]; then
    ACTIVE_PORT="10808"
    IS_HTTP="1"
    if command -v notify-send >/dev/null 2>&1; then
        notify-send "Antigravity" "⚠️ پروکسی فعالی شناسایی نشد؛ در حال استفاده از پورت پیش‌فرض 10808..." -i dialog-warning -t 3000 &
    fi
fi

if [ "$IS_HTTP" = "1" ]; then
    export ALL_PROXY="http://127.0.0.1:${ACTIVE_PORT}"
    export all_proxy="http://127.0.0.1:${ACTIVE_PORT}"
    export HTTPS_PROXY="http://127.0.0.1:${ACTIVE_PORT}"
    export https_proxy="http://127.0.0.1:${ACTIVE_PORT}"
    export HTTP_PROXY="http://127.0.0.1:${ACTIVE_PORT}"
    export http_proxy="http://127.0.0.1:${ACTIVE_PORT}"
    CHROMIUM_PROXY="http://127.0.0.1:${ACTIVE_PORT}"
else
    export ALL_PROXY="socks5h://127.0.0.1:${ACTIVE_PORT}"
    export all_proxy="socks5h://127.0.0.1:${ACTIVE_PORT}"
    CHROMIUM_PROXY="socks5://127.0.0.1:${ACTIVE_PORT}"
fi

export NO_PROXY="${NO_PROXY:-localhost,127.0.0.1,::1}"
export no_proxy="$NO_PROXY"

# Pass proxy without quotes to Chromium (Chromium does not support socks5h)
# Redirect stdout/stderr to /dev/null to prevent electron-log write EIO broken-pipe popups
exec "$APP_PATH" --proxy-server="$CHROMIUM_PROXY" --proxy-bypass-list="localhost;127.0.0.1;::1" "$@" >/dev/null 2>&1
`, appPath)

		_ = os.WriteFile(smartLauncherScript, []byte(scriptContent), 0755)

		// 2. Setup icon
		iconCandidates := []string{
			filepath.Join(appDir, "icon.png"),
			filepath.Join(filepath.Dir(appDir), "icon.png"),
			filepath.Join(appDir, "antigravity.png"),
			filepath.Join(filepath.Dir(appDir), "antigravity.png"),
			filepath.Join(filepath.Dir(appDir), "resources", "app", "resources", "linux", "code.png"),
			"/usr/share/pixmaps/antigravity.png",
		}

		var iconPath string
		for _, cand := range iconCandidates {
			if fi, err := os.Stat(cand); err == nil && fi.Size() >= 100 {
				iconPath = cand
				break
			}
		}

		if iconPath == "" {
			userIcon := filepath.Join(home, ".local", "share", "icons", "antigravity.png")
			iconPath = EnsureDefaultIcon(userIcon)
			hicolorIcon := filepath.Join(home, ".local", "share", "icons", "hicolor", "256x256", "apps", "antigravity.png")
			EnsureDefaultIcon(hicolorIcon)
		}

		// 3. Create single Antigravity IDE Desktop Launcher
		appMenuPath := filepath.Join(desktopAppDir, "antigravity.desktop")
		contentAntigravity := fmt.Sprintf(`[Desktop Entry]
Version=1.0
Name=Antigravity
GenericName=AI Code Editor
Comment=Google Antigravity with Smart No-TUN Proxy
Exec=%s %%F
Icon=%s
Path=%s
Terminal=false
Type=Application
Categories=Development;IDE;
StartupNotify=true
StartupWMClass=antigravity
`, smartLauncherScript, iconPath, workDir)

		if err := os.WriteFile(appMenuPath, []byte(contentAntigravity), 0755); err != nil {
			return "", err
		}

		// 4. Also create on ~/Desktop if Desktop directory exists
		desktopDir := filepath.Join(home, "Desktop")
		createdPaths := appMenuPath
		if fi, err := os.Stat(desktopDir); err == nil && fi.IsDir() {
			desktopShortcut := filepath.Join(desktopDir, "Antigravity.desktop")
			_ = os.WriteFile(desktopShortcut, []byte(contentAntigravity), 0755)
			_ = exec.Command("gio", "set", desktopShortcut, "metadata::trusted", "true").Run()

			createdPaths = fmt.Sprintf("%s and Desktop shortcut", appMenuPath)
		}

		// Refresh GNOME/KDE application database
		_ = exec.Command("update-desktop-database", desktopAppDir).Run()

		return createdPaths, nil
	}

	if runtime.GOOS == "darwin" {
		cmdPath := filepath.Join(home, "Desktop", "Antigravity-Proxy.command")
		content := fmt.Sprintf(`#!/usr/bin/env bash
# macOS Smart Proxy Launcher for Google Antigravity
APP_PATH="%s"
if [ ! -f "$APP_PATH" ]; then
    APP_PATH="/Applications/Antigravity.app/Contents/MacOS/Antigravity"
fi
if [ ! -f "$APP_PATH" ]; then
    APP_PATH="/Applications/Antigravity.app/Contents/MacOS/Electron"
fi

CANDIDATES=("10808" "7890" "2080" "10809" "2081" "1080")
ACTIVE_PORT=""
IS_HTTP="0"
for port in "${CANDIDATES[@]}"; do
    if exec 3<>/dev/tcp/127.0.0.1/$port 2>/dev/null; then
        ACTIVE_PORT="$port"
        printf "CONNECT cloudcode-pa.googleapis.com:443 HTTP/1.1\r\nHost: cloudcode-pa.googleapis.com:443\r\n\r\n" >&3
        read -t 0.3 -u 3 resp || true
        exec 3>&- 2>/dev/null || true
        if [[ "$resp" == HTTP/*200* ]] || [[ "$resp" == HTTP/* ]]; then
            IS_HTTP="1"
        fi
        break
    fi
done

if [ -z "$ACTIVE_PORT" ]; then
    ACTIVE_PORT="10808"
    IS_HTTP="1"
fi

if [ "$IS_HTTP" = "1" ]; then
    export ALL_PROXY="http://127.0.0.1:${ACTIVE_PORT}"
    export all_proxy="http://127.0.0.1:${ACTIVE_PORT}"
    export HTTPS_PROXY="http://127.0.0.1:${ACTIVE_PORT}"
    export https_proxy="http://127.0.0.1:${ACTIVE_PORT}"
    export HTTP_PROXY="http://127.0.0.1:${ACTIVE_PORT}"
    export http_proxy="http://127.0.0.1:${ACTIVE_PORT}"
    CHROMIUM_PROXY="http://127.0.0.1:${ACTIVE_PORT}"
else
    export ALL_PROXY="socks5h://127.0.0.1:${ACTIVE_PORT}"
    export all_proxy="socks5h://127.0.0.1:${ACTIVE_PORT}"
    CHROMIUM_PROXY="socks5://127.0.0.1:${ACTIVE_PORT}"
fi

export NO_PROXY="${NO_PROXY:-localhost,127.0.0.1,::1}"
export no_proxy="$NO_PROXY"

exec "$APP_PATH" --proxy-server="$CHROMIUM_PROXY" --proxy-bypass-list="localhost;127.0.0.1;::1" "$@"
`, appPath)

		if err := os.WriteFile(cmdPath, []byte(content), 0755); err != nil {
			return "", err
		}
		return cmdPath, nil
	}

	if runtime.GOOS == "windows" {
		cleanerAppDir := filepath.Join(home, "AppData", "Roaming", "antigravity-cleaner")
		_ = os.MkdirAll(cleanerAppDir, 0755)

		ps1Path := filepath.Join(cleanerAppDir, "launcher.ps1")
		vbsPath := filepath.Join(cleanerAppDir, "run.vbs")
		batPath := filepath.Join(cleanerAppDir, "launcher.bat")

		// 1. Dynamic silent PowerShell launcher with registry/env/port auto-detection (no hardcoded port or SSH spoofing)
		launcherPs1Content := fmt.Sprintf(`param([string]$TargetAppPath = "%s")
$ErrorActionPreference = 'SilentlyContinue'

$activeProxy = $null
$isHttp = $true

function Test-ProxyConnect([string]$hostName, [int]$port) {
    $t = New-Object System.Net.Sockets.TcpClient
    try {
        $iar = $t.BeginConnect($hostName, $port, $null, $null)
        if ($iar.AsyncWaitHandle.WaitOne(120, $false) -and $t.Connected) {
            $stream = $t.GetStream()
            $stream.ReadTimeout = 600
            $stream.WriteTimeout = 300
            $reqStr = "CONNECT cloudcode-pa.googleapis.com:443 HTTP/1.1" + [char]13 + [char]10 + "Host: cloudcode-pa.googleapis.com:443" + [char]13 + [char]10 + [char]13 + [char]10
            $req = [System.Text.Encoding]::ASCII.GetBytes($reqStr)
            $stream.Write($req, 0, $req.Length)
            $buf = New-Object byte[] 64
            $read = $stream.Read($buf, 0, 64)
            $resp = [System.Text.Encoding]::ASCII.GetString($buf, 0, $read)
            $httpMode = ($resp -match "^HTTP/\d\.\d 200" -or $resp -match "^HTTP/")
            return @{ Ok = $true; IsHttp = $httpMode }
        }
    } catch {} finally {
        $t.Close()
    }
    return @{ Ok = $false; IsHttp = $true }
}

# Priority 1: Explicit environment proxy variables
foreach ($envVal in @($env:HTTPS_PROXY, $env:HTTP_PROXY, $env:ALL_PROXY)) {
    if ($envVal -and $envVal -match '^(?:(https?|socks5h?)://)?([^:/]+):(\d+)') {
        $scheme = $Matches[1]
        $h = $Matches[2]; $p = [int]$Matches[3]
        $res = Test-ProxyConnect $h $p
        if ($res.Ok) {
            $activeProxy = "${h}:${p}"
            $isHttp = if ($scheme -like 'socks*') { $false } else { $res.IsHttp }
            break
        }
    }
}

# Priority 2: Windows System Proxy from Registry
if (-not $activeProxy) {
    $inet = Get-ItemProperty "HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings" -ErrorAction SilentlyContinue
    if ($inet -and $inet.ProxyEnable -eq 1 -and $inet.ProxyServer) {
        $srv = $inet.ProxyServer -replace '^https?://',''
        if ($srv -match '([^:;=\s]+):(\d+)') {
            $h = $Matches[1]; $p = [int]$Matches[2]
            $res = Test-ProxyConnect $h $p
            if ($res.Ok) {
                $activeProxy = "${h}:${p}"
                $isHttp = $res.IsHttp
            }
        }
    }
}

# Priority 3: Scan active local loopback proxy ports
if (-not $activeProxy) {
    foreach ($p in 10808, 7890, 2080, 10809, 2081, 1080) {
        $res = Test-ProxyConnect "127.0.0.1" $p
        if ($res.Ok) {
            $activeProxy = "127.0.0.1:${p}"
            $isHttp = $res.IsHttp
            break
        }
    }
}

$argList = @()
if ($activeProxy) {
    if ($isHttp) {
        $env:ALL_PROXY   = "http://$activeProxy"
        $env:all_proxy   = "http://$activeProxy"
        $env:HTTP_PROXY  = "http://$activeProxy"
        $env:http_proxy  = "http://$activeProxy"
        $env:HTTPS_PROXY = "http://$activeProxy"
        $env:https_proxy = "http://$activeProxy"
        $argList += "--proxy-server=http://$activeProxy"
    } else {
        $env:ALL_PROXY   = "socks5h://$activeProxy"
        $env:all_proxy   = "socks5h://$activeProxy"
        $argList += "--proxy-server=socks5://$activeProxy"
    }
    $env:NO_PROXY = "localhost,127.0.0.1,::1"
    $env:no_proxy = "localhost,127.0.0.1,::1"
    $argList += '--proxy-bypass-list=localhost;127.0.0.1;::1'
}

$appPath = $TargetAppPath
if (-not $appPath -or -not (Test-Path $appPath)) {
    $appPath = "%s"
}
if (-not (Test-Path $appPath)) {
    $appPath = "$env:LOCALAPPDATA\Programs\antigravity\Antigravity.exe"
}

if ($argList.Count -gt 0) {
    Start-Process -FilePath $appPath -ArgumentList $argList -WorkingDirectory (Split-Path $appPath)
} else {
    Start-Process -FilePath $appPath -WorkingDirectory (Split-Path $appPath)
}
`, appPath, appPath)
		_ = os.WriteFile(ps1Path, []byte(launcherPs1Content), 0755)

		// 2. Silent VBS runner (Zero console window flash)
		vbsContent := fmt.Sprintf(`Set shell = CreateObject("WScript.Shell")
ps1Path = "%s"
targetApp = ""
If WScript.Arguments.Count > 0 Then
    targetApp = WScript.Arguments(0)
End If
cmd = "powershell.exe -NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File """ & ps1Path & """ -TargetAppPath """ & targetApp & """"
shell.Run cmd, 0, False
`, ps1Path)
		_ = os.WriteFile(vbsPath, []byte(vbsContent), 0755)

		// 3. Resilient Standalone Batch Launcher (delegates dynamically to launcher.ps1; zero hardcoded ports)
		batContent := fmt.Sprintf(`@echo off
title Antigravity - Smart Proxy Launcher
powershell.exe -NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File "%%~dp0launcher.ps1" -TargetAppPath "%s"
`, appPath)
		_ = os.WriteFile(batPath, []byte(batContent), 0755)

		// 4. Generate & patch all Windows shortcuts (.lnk) across Desktop, Start Menu, and Taskbar Pinned
		targets := FindAllAntigravityExecutables()
		if len(targets) == 0 {
			targets = []AppTarget{{Name: "Antigravity", ExePath: appPath}}
		}

		for _, t := range targets {
			lnkPath := filepath.Join(home, "Desktop", fmt.Sprintf("%s.lnk", t.Name))
			lnkScript := fmt.Sprintf(`
$WshShell = New-Object -ComObject WScript.Shell
$vbsPath = "%s"
$exePath = "%s"
$workDir = "%s"
$appName = "%s"

function Set-SmartShortcut([string]$path) {
    try {
        $s = $WshShell.CreateShortcut($path)
        $s.TargetPath = "wscript.exe"
        $s.Arguments = """$vbsPath"" ""$exePath"""
        $s.WorkingDirectory = $workDir
        $s.IconLocation = "$exePath,0"
        $s.Description = "Google $appName with Smart Proxy"
        $s.Save()
    } catch {}
}

# Ensure primary Desktop shortcut
Set-SmartShortcut "%s"

# Recursively patch all existing shortcuts across Desktop, Start Menu, and Taskbar Pinned
$roots = @(
    [Environment]::GetFolderPath("Desktop"),
    [Environment]::GetFolderPath("CommonDesktopDirectory"),
    [Environment]::GetFolderPath("StartMenu"),
    [Environment]::GetFolderPath("CommonStartMenu"),
    (Join-Path $env:APPDATA "Microsoft\Internet Explorer\Quick Launch\User Pinned\TaskBar"),
    (Join-Path $env:APPDATA "Microsoft\Internet Explorer\Quick Launch\User Pinned\StartMenu")
) | Where-Object { $_ -and (Test-Path $_) }

foreach ($root in $roots) {
    Get-ChildItem -Path $root -Recurse -Filter "*.lnk" -ErrorAction SilentlyContinue | ForEach-Object {
        if ($_.Name -like "*Cleaner*") { return }
        try {
            $sc = $WshShell.CreateShortcut($_.FullName)
            $tLower = if ($sc.TargetPath) { $sc.TargetPath.ToLowerInvariant() } else { "" }
            $aLower = if ($sc.Arguments) { $sc.Arguments.ToLowerInvariant() } else { "" }
            $eLower = $exePath.ToLowerInvariant()
            if ($tLower -eq $eLower -or ($tLower -like "*wscript.exe" -and $aLower -like "*$eLower*")) {
                Set-SmartShortcut $_.FullName
            }
        } catch {}
    }
}
`, vbsPath, t.ExePath, filepath.Dir(t.ExePath), t.Name, lnkPath)
			_ = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", lnkScript).Run()
		}

		if len(targets) > 1 {
			return "Official Desktop, Start Menu & Taskbar shortcuts updated for Antigravity & Antigravity IDE", nil
		}
		return "Official Desktop, Start Menu & Taskbar shortcuts successfully patched with Smart No-TUN Proxy", nil
	}

	return "", fmt.Errorf("desktop launcher generation not supported on %s", runtime.GOOS)
}
