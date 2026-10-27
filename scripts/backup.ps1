[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'
Push-Location (Split-Path -Parent $PSScriptRoot)
try {
    New-Item -ItemType Directory -Path 'backups' -Force | Out-Null
    $backupDirectory = Get-Item -LiteralPath 'backups'
    if ($backupDirectory.Attributes -band [IO.FileAttributes]::ReparsePoint) {
        throw 'Use a real backup directory, not a junction or symbolic link.'
    }
    if ($env:OS -eq 'Windows_NT') {
        $owner = [Security.Principal.WindowsIdentity]::GetCurrent().User
        $acl = New-Object Security.AccessControl.DirectorySecurity
        $acl.SetAccessRuleProtection($true, $false)
        $acl.SetOwner($owner)
        $acl.AddAccessRule((New-Object Security.AccessControl.FileSystemAccessRule($owner, 'FullControl', 'ContainerInherit,ObjectInherit', 'None', 'Allow')))
        Set-Acl -LiteralPath $backupDirectory.FullName -AclObject $acl
    } else {
        & chmod 700 $backupDirectory.FullName
        if ($LASTEXITCODE -ne 0) { throw 'Cannot protect the backup directory.' }
    }
    $running = @(& docker compose ps --services --status running)
    if ($LASTEXITCODE -ne 0) { throw 'Cannot inspect this Compose project.' }
    $writers = @($running | Where-Object { $_ -in @('gateway', 'user', 'admin', 'api') })
    & docker compose --profile maintenance build maintenance
    if ($LASTEXITCODE -ne 0) { throw 'Cannot build the backup tool.' }
    try {
        if ($writers.Count -gt 0) {
            & docker compose stop @writers
            if ($LASTEXITCODE -ne 0) { throw 'Cannot stop application writers.' }
        }
        & docker compose --profile maintenance run --rm -e PICPILOT_BACKUP_QUIESCED=yes maintenance backup
        if ($LASTEXITCODE -ne 0) { throw 'Backup failed. An incomplete directory may remain; do not restore it.' }
    } finally {
        if ($writers.Count -gt 0) {
            & docker compose start @writers
            if ($LASTEXITCODE -ne 0) { Write-Warning 'Application restart failed; inspect docker compose ps.' }
        }
    }
} finally { Pop-Location }
