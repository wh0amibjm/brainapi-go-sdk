#!/usr/bin/env pwsh
<#
.SYNOPSIS
  One-command setup for the `brainapi` Agent Skill (Windows / PowerShell).

.DESCRIPTION
  Installs the skill into a Claude Code skills directory and runs a quick
  environment doctor (binary / jq / credentials) with copy-pasteable fixes for
  anything missing. PowerShell counterpart of install.sh; targets Windows
  PowerShell 5.1+ and PowerShell 7+.

  From a clone:   pwsh clients/skill/install.ps1
                  powershell -ExecutionPolicy Bypass -File clients\skill\install.ps1
  Public one-liner once the repo is public:
                  irm https://raw.githubusercontent.com/wh0amibjm/brainapi-go-sdk/main/clients/skill/install.ps1 | iex

.PARAMETER Global
  Install to $HOME\.claude\skills\brainapi (default).
.PARAMETER Project
  Install to <Dir>\.claude\skills\brainapi instead.
.PARAMETER Dir
  Project directory for -Project (defaults to the current directory).
.PARAMETER Ref
  Branch/tag to fetch from in remote mode (default: main).
#>
[CmdletBinding()]
param(
  [switch]$Global,
  [switch]$Project,
  [string]$Dir,
  [string]$Ref = 'main',
  [switch]$Help
)

$ErrorActionPreference = 'Stop'
$Repo = 'wh0amibjm/brainapi-go-sdk'
$SkillName = 'brainapi'
# Files that make up the skill (relative to clients/skill/). Update if the skill
# gains files — only matters for the remote (irm) install path.
$SkillFiles = @('brainapi/SKILL.md', 'brainapi/scripts/safe-submit.sh')

function Write-Info($m) { Write-Host "==> $m" -ForegroundColor Cyan }
function Write-Ok($m)   { Write-Host "  [ok] $m" -ForegroundColor Green }
function Write-Warn2($m){ Write-Host "  [! ] $m" -ForegroundColor Yellow }
function Die($m) { Write-Host "install: $m" -ForegroundColor Red; exit 2 }

if ($Help) {
  Write-Host "Usage: install.ps1 [-Global] [-Project [-Dir <path>]] [-Ref <git-ref>]"
  Write-Host "  -Global          install to ~/.claude/skills/brainapi (default)"
  Write-Host "  -Project -Dir D  install to D/.claude/skills/brainapi (D defaults to cwd)"
  Write-Host "  -Ref <ref>       branch/tag for remote (irm) install (default: main)"
  exit 0
}

# Resolve the skills directory and final target.
if ($Project) {
  $base = if ($Dir) { $Dir } else { (Get-Location).Path }
  if (-not (Test-Path -LiteralPath $base -PathType Container)) { Die "project dir not found: $base" }
  $skillsDir = Join-Path (Join-Path $base '.claude') 'skills'
} else {
  if (-not $HOME) { Die "`$HOME not set" }
  $skillsDir = Join-Path (Join-Path $HOME '.claude') 'skills'
}
$target = Join-Path $skillsDir $SkillName
# Guard the delete: target must be .../skills/brainapi.
if (((Split-Path $target -Leaf) -ne $SkillName) -or ((Split-Path (Split-Path $target -Parent) -Leaf) -ne 'skills')) {
  Die "refusing unsafe target: $target"
}

# Locate the skill source: local clone if present, else fetch from GitHub raw.
$src = $null
$tmp = $null
if ($PSScriptRoot -and (Test-Path -LiteralPath (Join-Path (Join-Path $PSScriptRoot $SkillName) 'SKILL.md'))) {
  $src = $PSScriptRoot
  Write-Info "Source: local clone ($src)"
} else {
  Write-Info "Source: GitHub raw ($Repo@$Ref)"
  $tmp = Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName())
  New-Item -ItemType Directory -Force -Path $tmp | Out-Null
  $src = $tmp
  $baseUrl = "https://raw.githubusercontent.com/$Repo/$Ref/clients/skill"
  foreach ($rel in $SkillFiles) {
    # Forward slashes are accepted by Windows .NET path APIs; no separator swap needed.
    $dest = Join-Path $src $rel
    New-Item -ItemType Directory -Force -Path (Split-Path $dest -Parent) | Out-Null
    try { Invoke-WebRequest -UseBasicParsing -Uri "$baseUrl/$rel" -OutFile $dest }
    catch { Die "fetch failed: $baseUrl/$rel" }
  }
}

# Install (idempotent: replace the skill dir wholesale).
Write-Info "Installing skill -> $target"
New-Item -ItemType Directory -Force -Path $skillsDir | Out-Null
if (Test-Path -LiteralPath $target) { Remove-Item -LiteralPath $target -Recurse -Force }
New-Item -ItemType Directory -Force -Path $target | Out-Null
Copy-Item -Path (Join-Path (Join-Path $src $SkillName) '*') -Destination $target -Recurse -Force
if ($tmp) { Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue }
Write-Ok "skill files installed"

# Doctor — surface prerequisites; don't fail the install over them.
Write-Info "Checking environment"
$bin = if ($env:BRAINAPI_BIN) { $env:BRAINAPI_BIN } else { 'brainapi' }
if (Get-Command $bin -ErrorAction SilentlyContinue) {
  $ver = ''
  try {
    $out = (& $bin version 2>$null | Out-String)
    if ($out -match '"version"\s*:\s*"([^"]+)"') { $ver = $Matches[1] }
  } catch { }
  if ($ver) { Write-Ok "brainapi found: $ver" } else { Write-Ok "brainapi found" }
} else {
  Write-Warn2 "brainapi not on PATH. Get it with:"
  Write-Host  "      go install github.com/$Repo/cmd/brainapi@latest"
  Write-Host  "      (or download a release .exe, then `$env:BRAINAPI_BIN = 'C:\path\to\brainapi.exe')"
}
if (Get-Command jq -ErrorAction SilentlyContinue) { Write-Ok "jq found" }
else { Write-Warn2 "jq not found (needed by safe-submit.sh) — install via winget/scoop/choco." }
if ($env:BRAINAPI_USER -and $env:BRAINAPI_PASS) { Write-Ok "BRAINAPI_USER / BRAINAPI_PASS set" }
else { Write-Warn2 "BRAINAPI_USER / BRAINAPI_PASS not set — set them (or pass --user/--pass) before live calls." }

Write-Host ""
Write-Info "Done. Open a new Claude Code session in this scope and ask about a BRAIN alpha,"
Write-Info "or invoke it explicitly with /$SkillName."
