#Requires -Version 7.0
param([ValidateSet('amd64','arm64')][string]$Arch = 'amd64')
$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path -Parent $PSScriptRoot
$taskOutput = Join-Path $taskRoot "release/linux-$Arch"
if (Test-Path -LiteralPath $taskOutput) {
    $taskResolved = (Resolve-Path -LiteralPath $taskOutput).Path
    $taskExpected = [IO.Path]::GetFullPath((Join-Path $taskRoot "release/linux-$Arch"))
    if ($taskResolved -ne $taskExpected) { throw 'Unexpected release path' }
    Remove-Item -LiteralPath $taskResolved -Recurse -Force
}
New-Item -ItemType Directory -Path $taskOutput -Force | Out-Null
Push-Location $taskRoot
try {
    npm --prefix frontend ci
    if ($LASTEXITCODE -ne 0) { throw 'Frontend dependency installation failed' }
    npm --prefix frontend run typecheck
    if ($LASTEXITCODE -ne 0) { throw 'Frontend type check failed' }
    npm --prefix frontend run build
    if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed' }
    $taskOldOS = $env:GOOS; $taskOldArch = $env:GOARCH; $taskOldCGO = $env:CGO_ENABLED
    try {
        $env:GOOS='linux'; $env:GOARCH=$Arch; $env:CGO_ENABLED='0'
        go -C backend build -trimpath -ldflags '-s -w' -o "$taskOutput/asset-server" ./cmd/server
        if ($LASTEXITCODE -ne 0) { throw 'Go server build failed' }
        go -C backend build -trimpath -ldflags '-s -w' -o "$taskOutput/asset-migrate" ./cmd/migrate
        if ($LASTEXITCODE -ne 0) { throw 'Go migration build failed' }
    } finally { $env:GOOS=$taskOldOS; $env:GOARCH=$taskOldArch; $env:CGO_ENABLED=$taskOldCGO }
    Copy-Item -LiteralPath frontend/dist -Destination "$taskOutput/frontend" -Recurse -Force
    Copy-Item -LiteralPath deploy -Destination "$taskOutput/deploy" -Recurse -Force
    $taskEntries = Get-ChildItem -LiteralPath $taskOutput -File -Recurse | Where-Object Name -ne 'SHA256SUMS.txt' | Sort-Object FullName | ForEach-Object {
        $taskHash = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLower()
        $taskRelative = [IO.Path]::GetRelativePath($taskOutput, $_.FullName).Replace('\','/')
        "$taskHash  $taskRelative"
    }
    $taskEntries | Set-Content -LiteralPath "$taskOutput/SHA256SUMS.txt" -Encoding utf8NoBOM
    Write-Output "Release ready: $taskOutput"
} finally { Pop-Location }
