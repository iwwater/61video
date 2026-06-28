# 91 smoke 测试。
#
# 起 launcher (local-only) → 等前后端端口 → HTTP 验关键路径 → 杀进程。
# 适用：每次提交后 / 重大改动后 / 启动 launcher 之前手动回归。
#
# 关键不变量：
#   - 6191 (frontend) 200
#   - 6192 (backend) /api/list 200（带 session 也行，未登录返回 401/403）
#   - 6192 /admin/login 200
#   - 进程干净退出

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
$launcher = Join-Path $repoRoot "61-start.exe"
$tmpDir = Join-Path $repoRoot "tmp"
$frontendPort = 6191
$backendPort = 6192
$timeoutSec = 45

if (-not (Test-Path $launcher)) {
    throw "Missing launcher: $launcher. Run scripts\build-91-start.ps1 first."
}

if (-not (Test-Path $tmpDir)) {
    New-Item -ItemType Directory -Path $tmpDir | Out-Null
}

# 启动前先确认端口没被占用——smoke 是隔离的，不应影响其他 91 实例
function Test-PortOpen {
    param([int]$Port)
    $client = $null
    try {
        $client = [System.Net.Sockets.TcpClient]::new()
        $iar = $client.BeginConnect("127.0.0.1", $Port, $null, $null)
        $connected = $iar.AsyncWaitHandle.WaitOne(500) -and $client.Connected
        if ($connected) { $client.EndConnect($iar) }
        return $connected
    }
    catch { return $false }
    finally { if ($client) { $client.Dispose() } }
}

if (Test-PortOpen $frontendPort) {
    throw "Port $frontendPort is in use. Stop existing 91 instance before running smoke."
}
if (Test-PortOpen $backendPort) {
    throw "Port $backendPort is in use. Stop existing 91 instance before running smoke."
}

Write-Host "[smoke] launching 61-start.exe --mode local-only"
$launcherLog = Join-Path $tmpDir "smoke-launcher-stdout.log"
$launcherErr = Join-Path $tmpDir "smoke-launcher-stderr.log"
$proc = Start-Process -FilePath $launcher -ArgumentList "--mode", "local-only" `
    -RedirectStandardOutput $launcherLog -RedirectStandardError $launcherErr `
    -PassThru -NoNewWindow

function Wait-Port {
    param([int]$Port)
    $deadline = (Get-Date).AddSeconds($timeoutSec)
    while ((Get-Date) -lt $deadline) {
        if (Test-PortOpen $Port) {
            return
        }
        Start-Sleep -Milliseconds 500
    }
    throw "Timed out waiting for port $Port after ${timeoutSec}s. Launcher stdout: $(Get-Content $launcherLog -Raw -ErrorAction SilentlyContinue)"
}

function Test-Http {
    param(
        [string]$Url,
        [int[]]$ExpectedStatus = @(200),
        [string]$Label
    )
    $allowed = $ExpectedStatus
    try {
        $resp = Invoke-WebRequest -Uri $Url -UseBasicParsing -TimeoutSec 10 -Method Get
        if ($allowed -contains $resp.StatusCode) {
            Write-Host "[smoke] $Label => $($resp.StatusCode) OK"
        }
        else {
            throw "$Label expected one of [$($allowed -join ',')], got $($resp.StatusCode)"
        }
    }
    catch [System.Net.WebException] {
        $code = $_.Exception.Response.StatusCode.value__
        if ($allowed -contains $code) {
            Write-Host "[smoke] $Label => $code OK (via WebException)"
        }
        else {
            throw "$Label expected one of [$($allowed -join ',')], got $code. Error: $($_.Exception.Message)"
        }
    }
}

# cleanupPorts: kill anything still listening on the smoke ports. Called
# from the finally block; safe to run multiple times.
function cleanupPorts {
    foreach ($port in @($frontendPort, $backendPort)) {
        $lines = netstat -ano
        $pids = @()
        foreach ($line in $lines) {
            if ($line -match "127\.0\.0\.1:$port\s.*LISTENING\s+(\d+)$") {
                $pids += $Matches[1]
            }
        }
        foreach ($pid in ($pids | Sort-Object -Unique)) {
            if ($pid -ne "0") {
                Write-Host "[smoke] killing orphaned child on port $port PID=$pid"
                cmd /c "taskkill /T /F /PID $pid" 2>&1 | Out-Null
            }
        }
    }
}

$cleanup = {
    if ($proc -and -not $proc.HasExited) {
        Write-Host "[smoke] killing launcher PID=$($proc.Id)"
        Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
        $proc.WaitForExit(5000) | Out-Null
    }
    # smoke 跑完后端口必须空出来，否则下次 smoke 启动就拒绝。launcher
    # 是 console 进程 spawn 出来的，没用 job object；launcher 被 -Force
    # 杀掉后，video-server.exe / node.exe (vite preview) 会变孤儿继续
    # 监听 6191/6192。taskkill /T 把整个进程树带走。
    cleanupPorts
}

try {
    Write-Host "[smoke] waiting for backend port $backendPort"
    Wait-Port $backendPort
    Write-Host "[smoke] waiting for frontend port $frontendPort"
    Wait-Port $frontendPort

    # 顺序：先 backend 健康检查 → 再 frontend 主页 → admin 登录页
    # 注意 admin 路径是 frontend 通过 vite 反代的，所以两个端口都要测。
    # /api/list 走 auth.Required：未登录返回 401，登录后返回 200；smoke
    # 不带 session 跑，401 即说明 backend + 路由 + auth 中间件都活着。
    Test-Http -Url "http://127.0.0.1:$backendPort/api/list" -ExpectedStatus @(200, 401) -Label "backend /api/list"
    Test-Http -Url "http://127.0.0.1:$frontendPort/" -ExpectedStatus @(200) -Label "frontend /"
    Test-Http -Url "http://127.0.0.1:$frontendPort/admin" -ExpectedStatus @(200) -Label "frontend /admin"

    Write-Host "[smoke] all checks passed"
}
catch {
    Write-Host "[smoke] FAILED: $_" -ForegroundColor Red
    Write-Host "[smoke] launcher stdout:" -ForegroundColor Yellow
    if (Test-Path $launcherLog) { Get-Content $launcherLog | Select-Object -Last 30 }
    Write-Host "[smoke] launcher stderr:" -ForegroundColor Yellow
    if (Test-Path $launcherErr) { Get-Content $launcherErr | Select-Object -Last 30 }
    throw
}
finally {
    & $cleanup
}