#Requires -Version 7.0
param([string]$Destination = 'D:\九吨资产备份')
$ErrorActionPreference = 'Stop'
$backupRoot = [IO.Path]::GetFullPath($Destination)
if (!(Test-Path -LiteralPath $backupRoot -PathType Container)) { throw 'Backup folder must be provisioned first' }
$control = Join-Path $backupRoot '.connection'
$statusFile = Join-Path $backupRoot '备份状态.json'
$backupPartial = $null
function Invoke-BackupSSH([string]$Operation, [string]$OutputFile = '') {
    $processInfo = [Diagnostics.ProcessStartInfo]::new()
    $processInfo.FileName = "$env:WINDIR\System32\OpenSSH\ssh.exe"
    $processInfo.UseShellExecute = $false
    $processInfo.CreateNoWindow = $true
    $processInfo.RedirectStandardOutput = $true
    $processInfo.RedirectStandardError = $true
    foreach ($argument in @('-T','-o','BatchMode=yes','-o','IdentitiesOnly=yes','-o','StrictHostKeyChecking=yes','-o',"UserKnownHostsFile=$(Join-Path $control 'known_hosts')",'-o','ConnectTimeout=10','-o','ServerAliveInterval=10','-o','ServerAliveCountMax=2','-i',(Join-Path $control 'id_ed25519'),'asset-backup-pull@120.53.13.231',$Operation)) { $processInfo.ArgumentList.Add($argument) }
    $process = [Diagnostics.Process]::Start($processInfo)
    $stream = $null
    try {
        $errorRead = $process.StandardError.ReadToEndAsync()
        if ($OutputFile) {
            $stream = [IO.File]::Open($OutputFile,[IO.FileMode]::CreateNew)
            $outputRead = $process.StandardOutput.BaseStream.CopyToAsync($stream)
            $resultText = ''
        } else { $outputRead = $process.StandardOutput.ReadToEndAsync() }
        if (!$process.WaitForExit(60000)) { $process.Kill($true); throw 'Backup connection timed out' }
        $outputRead.GetAwaiter().GetResult() | Out-Null
        if (!$OutputFile) { $resultText = $outputRead.Result }
        if ($process.ExitCode -ne 0) { throw ('Backup download failed: ' + $errorRead.GetAwaiter().GetResult().Trim()) }
        return $resultText
    } finally { if ($stream) { $stream.Dispose() }; $process.Dispose() }
}
try {
    $metadata = (Invoke-BackupSSH 'metadata') | ConvertFrom-Json
    if ($metadata.name -cnotmatch '^asset-system-\d{8}T\d{6}Z\.sql\.gz$' -or $metadata.sha256 -cnotmatch '^[a-f0-9]{64}$' -or $metadata.size -le 0) { throw 'Invalid backup metadata' }
    $stamp = [datetime]::ParseExact($metadata.name.Substring(13,16),'yyyyMMddTHHmmssZ',[Globalization.CultureInfo]::InvariantCulture,[Globalization.DateTimeStyles]::AssumeUniversal).ToUniversalTime()
    if ([datetime]::UtcNow - $stamp -gt [timespan]::FromHours(48)) { throw 'The latest server backup is older than 48 hours' }
    $backupFile = Join-Path $backupRoot $metadata.name
    if (!(Test-Path -LiteralPath $backupFile) -or (Get-FileHash -LiteralPath $backupFile -Algorithm SHA256).Hash.ToLowerInvariant() -ne $metadata.sha256) {
        $backupPartial = "$backupFile.$([guid]::NewGuid().ToString('N')).part"
        Invoke-BackupSSH "export $($metadata.name)" $backupPartial | Out-Null
        if ((Get-Item -LiteralPath $backupPartial).Length -ne $metadata.size -or (Get-FileHash -LiteralPath $backupPartial -Algorithm SHA256).Hash.ToLowerInvariant() -ne $metadata.sha256) { throw 'Backup size or SHA256 mismatch' }
        $compressed = [IO.File]::OpenRead($backupPartial)
        try {
            $decompressor = [IO.Compression.GZipStream]::new($compressed,[IO.Compression.CompressionMode]::Decompress)
            try { $buffer = [byte[]]::new(65536); while ($decompressor.Read($buffer,0,$buffer.Length) -gt 0) {} } finally { $decompressor.Dispose() }
        } finally { $compressed.Dispose() }
        Move-Item -LiteralPath $backupPartial -Destination $backupFile -Force
        $backupPartial = $null
    }
    # Only delete dated backup files directly inside the verified destination.
    foreach ($candidate in Get-ChildItem -LiteralPath $backupRoot -File) {
        if ($candidate.Name -cmatch '^asset-system-(\d{8}T\d{6}Z)\.sql\.gz$') {
            $candidateStamp = [datetime]::ParseExact($Matches[1],'yyyyMMddTHHmmssZ',[Globalization.CultureInfo]::InvariantCulture,[Globalization.DateTimeStyles]::AssumeUniversal)
            if ($candidateStamp -lt [datetime]::UtcNow.AddDays(-30) -and $candidate.Name -ne $metadata.name) {
                if ([IO.Path]::GetFullPath($candidate.DirectoryName) -ne $backupRoot) { throw 'Unexpected retention path' }
                Remove-Item -LiteralPath $candidate.FullName -Force
            }
        }
    }
    @{status='success';checkedAt=[datetime]::Now.ToString('o');file=$metadata.name;bytes=$metadata.size;sha256=$metadata.sha256;retentionDays=30} | ConvertTo-Json | Set-Content -LiteralPath $statusFile -Encoding utf8NoBOM
    Write-Output "Backup verified: $($metadata.name), $($metadata.size) bytes"
} catch {
    if ($backupPartial -and (Test-Path -LiteralPath $backupPartial)) { Remove-Item -LiteralPath $backupPartial -Force }
    @{status='failed';checkedAt=[datetime]::Now.ToString('o');message=$_.Exception.Message} | ConvertTo-Json | Set-Content -LiteralPath $statusFile -Encoding utf8NoBOM
    throw
}
