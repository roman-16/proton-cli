<#
.SYNOPSIS
  proton-cli installer for Windows.

.DESCRIPTION
  Downloads the latest proton-cli release from GitHub Releases, verifies its
  SHA-256 checksum, and installs it into a user directory as `proton`, with
  `proton-cli` beside it as a second name. No package manager required.
  (winget remains the recommended Windows channel:
  `winget install Roman-16.ProtonCLI`.)

.EXAMPLE
  irm https://raw.githubusercontent.com/roman-16/proton-cli/main/scripts/install.ps1 | iex

.EXAMPLE
  # Pin a version or install directory (run the script directly, not piped):
  .\install.ps1 -Version 1.9.11 -InstallDir "C:\tools\proton-cli"
#>
[CmdletBinding()]
param(
    [string]$Version = $env:PROTON_CLI_VERSION,
    [string]$InstallDir = $(if ($env:PROTON_CLI_INSTALL_DIR) { $env:PROTON_CLI_INSTALL_DIR } else { "$env:LOCALAPPDATA\Programs\proton-cli" })
)

$ErrorActionPreference = 'Stop'
try { [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12 } catch {}

$repo = 'roman-16/proton-cli'
# The OS architecture, not the process one: an x64 PowerShell emulated on an
# ARM64 machine should still be handed the ARM64 binary.
$arch = if ([Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq [Runtime.InteropServices.Architecture]::Arm64) { 'arm64' } else { 'amd64' }
$asset = "proton-cli_windows_$arch.exe"

$base = if ($Version) {
    "https://github.com/$repo/releases/download/v$($Version.TrimStart('v'))"
} else {
    "https://github.com/$repo/releases/latest/download"
}

# The three notes the script makes, matching the sh installer and the binary:
# a tick for something that went right, a bang for something worth knowing.
function Write-Info { param([string]$Message) Write-Host "  $Message" }
function Write-Success { param([string]$Message)
    Write-Host '  ' -NoNewline
    Write-Host '✓' -ForegroundColor Green -NoNewline
    Write-Host " $Message"
}
function Write-Step { param([string]$Command, [string]$Purpose)
    Write-Host ("    {0,-32} " -f $Command) -NoNewline
    Write-Host $Purpose -ForegroundColor DarkGray
}

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("proton-cli-" + [Guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp -Force | Out-Null
try {
    Write-Info "Downloading $asset$(if ($Version) { " (v$($Version.TrimStart('v')))" })…"
    Invoke-WebRequest -Uri "$base/$asset" -OutFile "$tmp\$asset" -UseBasicParsing
    Invoke-WebRequest -Uri "$base/checksums.txt" -OutFile "$tmp\checksums.txt" -UseBasicParsing

    $expected = (Select-String -Path "$tmp\checksums.txt" -Pattern "\s$([regex]::Escape($asset))$" |
        Select-Object -First 1).Line -split '\s+' | Select-Object -First 1
    if (-not $expected) { throw "no checksum entry for $asset in checksums.txt" }
    $actual = (Get-FileHash -Algorithm SHA256 -Path "$tmp\$asset").Hash.ToLower()
    if ($expected.ToLower() -ne $actual) { throw "checksum mismatch for $asset (expected $expected, got $actual)" }
    Write-Success 'Checksum verified'

    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    $dest = Join-Path $InstallDir 'proton.exe'
    Move-Item -Path "$tmp\$asset" -Destination $dest -Force

    # The binary answering is the only proof the install worked: a checksum says
    # the bytes arrived, not that they run on this machine. Its own words stay on
    # the far side of 2>$null, because a native command writing to stderr under
    # 2>&1 throws here before anything can read them.
    $reported = & $dest --version 2>$null
    if ($LASTEXITCODE -ne 0) {
        throw "proton.exe was installed to $dest but does not run on this machine (exit $LASTEXITCODE)."
    }

    # `--version` prints "proton version X.Y.Z"; the bare number reads better in
    # a sentence that already names the program.
    $installed = ($reported -split '\s+')[-1]
    Write-Success "Installed proton $installed → $dest"

    # An install answers to both names. Windows has no symlink an ordinary user
    # may create, so the second name is a shim that resolves the program
    # relative to its own directory and passes the exit code back out.
    $shim = Join-Path $InstallDir 'proton-cli.cmd'
    Set-Content -Path $shim -Value "@echo off`r`n`"%~dp0proton.exe`" %*" -NoNewline -Encoding ASCII
    Write-Success 'proton-cli → proton'
}
finally {
    Remove-Item -Path $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($userPath -split ';') -notcontains $InstallDir) {
    Write-Warning "$InstallDir is not on your PATH. To add it permanently, run:"
    Write-Host "  [Environment]::SetEnvironmentVariable('Path', `"`$([Environment]::GetEnvironmentVariable('Path','User'));$InstallDir`", 'User')"
    # Make proton usable in the current session without persisting anything.
    $env:Path = "$env:Path;$InstallDir"
}

# Close on what to do rather than on how to undo it: somebody who has just run an
# install command is looking for the first command, not the last one.
Write-Host ''
Write-Info 'Next:'
Write-Step 'proton account login' 'sign in'
Write-Step 'proton --help' 'what it can do'
Write-Step 'proton completion powershell' 'tab completion'
Write-Host ''
Write-Info 'Remove it again any time with: proton uninstall'
