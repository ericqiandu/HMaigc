[CmdletBinding()]
param(
  [Parameter(ValueFromRemainingArguments = $true)]
  [string[]] $ComposeArgs
)

$ErrorActionPreference = "Stop"

$sourceRoot = Split-Path -Parent $PSScriptRoot
$gitCommonDir = (& git -C $sourceRoot rev-parse --git-common-dir).Trim()
if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($gitCommonDir)) {
  throw "Cannot resolve the Git common directory; refusing to start the local stack."
}

if (-not [System.IO.Path]::IsPathRooted($gitCommonDir)) {
  $gitCommonDir = Join-Path $sourceRoot $gitCommonDir
}
$gitCommonDir = [System.IO.Path]::GetFullPath($gitCommonDir)
$mainRepositoryRoot = Split-Path -Parent $gitCommonDir
$canonicalDataPath = Join-Path $mainRepositoryRoot ".local\data"

if ($canonicalDataPath -match '[\\/]\.worktrees[\\/]') {
  throw "The data directory resolves inside a worktree; refusing to create a second local database: $canonicalDataPath"
}

New-Item -ItemType Directory -Path $canonicalDataPath -Force | Out-Null

if (-not $ComposeArgs -or $ComposeArgs.Count -eq 0) {
  $ComposeArgs = @("up", "-d", "--build", "--wait")
}

$previousDataPath = $env:CANVAS_DATA_PATH
try {
  $env:CANVAS_DATA_PATH = $canonicalDataPath
  Write-Host "Canonical local data directory: $canonicalDataPath"
  & docker compose --project-directory $sourceRoot --file (Join-Path $sourceRoot "docker-compose.yml") @ComposeArgs
  if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
  }
}
finally {
  if ($null -eq $previousDataPath) {
    Remove-Item Env:CANVAS_DATA_PATH -ErrorAction SilentlyContinue
  }
  else {
    $env:CANVAS_DATA_PATH = $previousDataPath
  }
}
