# Резервная копия базы (папка data/): пользователи, ключ сессий, кейсы и профили.
# Запуск: в PowerShell из корня проекта — powershell -ExecutionPolicy Bypass -File backup.ps1
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$stamp = Get-Date -Format 'yyyy-MM-dd_HHmmss'
$dest = Join-Path $root "backups\$stamp"
New-Item -ItemType Directory -Path $dest -Force | Out-Null
Copy-Item -Path (Join-Path $root 'data') -Destination $dest -Recurse -Force
Write-Host "Бэкап создан: $dest"
