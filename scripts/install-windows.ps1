<#
.SYNOPSIS
    Installs the NexusVPN client (nexusvpnctl.exe) on Windows.

.DESCRIPTION
    Builds nexusvpnctl from this checkout, installs it under Program Files,
    and adds it to the system PATH. Must be run from an elevated PowerShell
    session.

    Creating a TUN adapter on Windows also requires the Wintun driver
    (https://www.wintun.net/). Place wintun.dll next to nexusvpnctl.exe, or
    install it system-wide, before running 'nexusvpnctl up'.

.EXAMPLE
    .\install-windows.ps1
#>

#Requires -RunAsAdministrator
[CmdletBinding()]
param(
    [string]$InstallDir = "$env:ProgramFiles\NexusVPN"
)

$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw 'Go is required to build the client (https://go.dev/dl/).'
}

Write-Host '==> Building nexusvpnctl.exe'
Push-Location (Join-Path $repoRoot 'client')
try {
    $version = 'dev'
    try {
        $described = git -C $repoRoot describe --tags --always --dirty 2>$null
        if ($LASTEXITCODE -eq 0 -and $described) { $version = $described }
    } catch {
        # git is optional; fall back to the 'dev' version string.
    }

    $env:CGO_ENABLED = '0'
    go build -trimpath `
        -ldflags "-s -w -X main.Version=$version" `
        -o "$env:TEMP\nexusvpnctl.exe" ./cmd/nexusvpnctl
    if ($LASTEXITCODE -ne 0) { throw 'build failed' }
} finally {
    Pop-Location
}

Write-Host "==> Installing to $InstallDir"
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
Move-Item -Force "$env:TEMP\nexusvpnctl.exe" (Join-Path $InstallDir 'nexusvpnctl.exe')

# Add to the system PATH if it isn't already there.
$machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
if ($machinePath -notlike "*$InstallDir*") {
    Write-Host '==> Adding to system PATH'
    [Environment]::SetEnvironmentVariable('Path', "$machinePath;$InstallDir", 'Machine')
    Write-Host '    (open a new terminal for this to take effect)'
}

$wintun = Join-Path $InstallDir 'wintun.dll'
if (-not (Test-Path $wintun)) {
    Write-Warning @'
wintun.dll was not found in the install directory.

The client cannot create a tunnel adapter without it. Download the Wintun
driver from https://www.wintun.net/ and copy the DLL matching your CPU
architecture (bin\amd64\wintun.dll for x64) into:
'@
    Write-Warning "    $InstallDir"
}

Write-Host ''
Write-Host 'Installed. Next (from an elevated terminal):'
Write-Host '  nexusvpnctl login -server https://your-control-plane'
Write-Host '  nexusvpnctl network join -code <INVITE CODE>'
Write-Host '  nexusvpnctl up'
Write-Host ''
Write-Host "Creating the tunnel adapter requires Administrator, so run 'up' elevated."
