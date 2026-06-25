param(
    [switch]$SkipServe
)

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
$launcher = Join-Path $repoRoot "61-start.exe"

if (-not (Test-Path $launcher)) {
    throw "Missing launcher: $launcher"
}

$tailscale = Get-Command tailscale -ErrorAction SilentlyContinue
if (-not $tailscale) {
    throw "tailscale command was not found. Install Tailscale first."
}

Write-Host "Starting local 61 site..."
$launcherProcess = Start-Process -FilePath $launcher -PassThru

function Wait-Port {
    param(
        [string]$TargetHost = "127.0.0.1",
        [int]$Port,
        [int]$TimeoutSeconds = 45
    )

    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        $client = $null
        try {
            $client = [System.Net.Sockets.TcpClient]::new()
            $iar = $client.BeginConnect($TargetHost, $Port, $null, $null)
            if ($iar.AsyncWaitHandle.WaitOne(500) -and $client.Connected) {
                $client.EndConnect($iar)
                return
            }
        }
        catch {
        }
        finally {
            if ($client) { $client.Dispose() }
        }
        Start-Sleep -Milliseconds 500
    }
    throw "Timed out waiting for local site port $TargetHost`:$Port"
}

Wait-Port -Port 6191

$status = $null
try {
    $status = tailscale status --json | ConvertFrom-Json
}
catch {
    Write-Host "Tailscale is not connected. Running 'tailscale up'..."
    tailscale up
    $status = tailscale status --json | ConvertFrom-Json
}

if (-not $SkipServe) {
    Write-Host "Configuring Tailscale Serve -> local :6191 ..."
    tailscale serve --bg 6191 | Out-Null
}

$dnsName = ""
if ($status.Self -and $status.Self.DNSName) {
    $dnsName = $status.Self.DNSName.TrimEnd(".")
}
$tailIp = $null
if ($status.Self -and $status.Self.TailscaleIPs) {
    $tailIp = $status.Self.TailscaleIPs | Select-Object -First 1
}

Write-Host ""
Write-Host "Access URLs"
Write-Host "----------------------------------------"
Write-Host "Local home:   http://127.0.0.1:6191/"
Write-Host "Local admin:  http://127.0.0.1:6191/admin"
if ($dnsName) {
    Write-Host "Tailnet home: http://$dnsName`:6191/"
    Write-Host "Tailnet admin:http://$dnsName`:6191/admin"
    if (-not $SkipServe) {
        Write-Host "Serve home:   https://$dnsName/"
        Write-Host "Serve admin:  https://$dnsName/admin"
    }
}
if ($tailIp) {
    Write-Host "Tail IP home: http://$tailIp`:6191/"
    Write-Host "Tail IP admin:http://$tailIp`:6191/admin"
}
Write-Host ""
Write-Host "Launcher PID: $($launcherProcess.Id)"
Write-Host "Serve status: tailscale serve status"
