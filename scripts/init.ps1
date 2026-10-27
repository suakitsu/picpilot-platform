[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$target = Join-Path $root '.env'
if (Test-Path -LiteralPath $target) {
    Write-Output '.env already exists; nothing was changed.'
    exit 0
}
$edition = (Get-Content -LiteralPath (Join-Path $root 'deploy/edition.txt') -Raw).Trim()
if ($edition -notin @('personal', 'platform')) { throw 'Invalid edition.' }
$random = [Security.Cryptography.RandomNumberGenerator]::Create()
try {
    $bytes = New-Object byte[] 32
    $random.GetBytes($bytes)
    $password = ([BitConverter]::ToString($bytes)).Replace('-', '').ToLowerInvariant()
} finally { $random.Dispose() }
$content = [IO.File]::ReadAllText((Join-Path $root '.env.example'))
$content = $content.Replace('POSTGRES_PASSWORD=', "POSTGRES_PASSWORD=$password")
$content = $content.Replace('COMPOSE_PROJECT_NAME=picpilot-platform', "COMPOSE_PROJECT_NAME=picpilot-$edition")
$temporary = Join-Path $root ('.env.init-' + [Guid]::NewGuid().ToString('N'))
[IO.File]::Open($temporary, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None).Dispose()
try {
    if ($env:OS -eq 'Windows_NT') {
        $acl = New-Object Security.AccessControl.FileSecurity
        $acl.SetAccessRuleProtection($true, $false)
        $owner = [Security.Principal.WindowsIdentity]::GetCurrent().User
        $acl.SetOwner($owner)
        $acl.AddAccessRule((New-Object Security.AccessControl.FileSystemAccessRule($owner, 'FullControl', 'Allow')))
        Set-Acl -LiteralPath $temporary -AclObject $acl
    } else {
        & chmod 600 $temporary
        if ($LASTEXITCODE -ne 0) { throw 'Cannot protect the environment file.' }
    }
    $data = [Text.Encoding]::UTF8.GetBytes($content.Replace("`r`n", "`n"))
    $stream = [IO.File]::Open($temporary, [IO.FileMode]::Open, [IO.FileAccess]::Write, [IO.FileShare]::None)
    try { $stream.Write($data, 0, $data.Length); $stream.Flush($true) } finally { $stream.Dispose() }
    [IO.File]::Move($temporary, $target)
} finally {
    if (Test-Path -LiteralPath $temporary) { Remove-Item -LiteralPath $temporary -Force }
    $password = $null
    $content = $null
}
Write-Output 'Created private .env with a random database password. No model provider is enabled.'
Write-Output 'Next: docker compose up -d --build'
