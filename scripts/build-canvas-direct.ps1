$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
$canvasRoots = @(
  (Join-Path $repoRoot 'third_party\nova-image-studio\frontend'),
  (Join-Path $repoRoot 'third_party\infinite-canvas\web')
)

foreach ($canvasRoot in $canvasRoots) {
  if (-not (Test-Path -LiteralPath (Join-Path $canvasRoot 'package-lock.json'))) {
    throw "package-lock.json not found: $canvasRoot"
  }
  Push-Location $canvasRoot
  try {
    & npm.cmd ci
    if ($LASTEXITCODE -ne 0) { throw "npm ci failed: $canvasRoot" }
    & npm.cmd run build
    if ($LASTEXITCODE -ne 0) { throw "canvas build failed: $canvasRoot" }
  } finally {
    Pop-Location
  }
}

Write-Host 'Canvas builds are ready for the direct Go/Next startup path.'
