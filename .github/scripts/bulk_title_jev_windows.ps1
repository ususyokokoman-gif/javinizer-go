param(
    [Parameter(Mandatory = $true)]
    [string]$InputRoot,

    [string]$BaseURL = "http://127.0.0.1:8080",

    [string]$OutputDir = "bulk-title-jev-output",

    [int]$BatchSize = 50,

    [int]$MaxInflightBatches = 4,

    [int]$QuickHashBytes = 1048576,

    [switch]$SkipDuplicateCheck
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

if ($BatchSize -lt 1) { throw "BatchSize must be >= 1" }
if ($MaxInflightBatches -lt 1) { throw "MaxInflightBatches must be >= 1" }
if ($QuickHashBytes -lt 65536) { throw "QuickHashBytes must be >= 65536" }

$root = (Resolve-Path -LiteralPath $InputRoot).Path
$out = [System.IO.Path]::GetFullPath((Join-Path $PWD $OutputDir))
New-Item -ItemType Directory -Path $out -Force | Out-Null

$extensions = @(".mp4", ".mkv", ".avi", ".wmv", ".flv", ".mov", ".m4v", ".ts", ".webm")

function Get-HexHash {
    param([byte[]]$Bytes)
    $sha = [System.Security.Cryptography.SHA256]::Create()
    try {
        return ([BitConverter]::ToString($sha.ComputeHash($Bytes))).Replace("-", "").ToLowerInvariant()
    } finally {
        $sha.Dispose()
    }
}

function Get-QuickHash {
    param(
        [Parameter(Mandatory = $true)][System.IO.FileInfo]$File,
        [int]$SampleBytes
    )

    $stream = [System.IO.File]::Open($File.FullName, [System.IO.FileMode]::Open, [System.IO.FileAccess]::Read, [System.IO.FileShare]::ReadWrite)
    try {
        $take = [Math]::Min([int64]$SampleBytes, $File.Length)
        $first = New-Object byte[] $take
        [void]$stream.Read($first, 0, $first.Length)

        $last = @()
        if ($File.Length -gt $take) {
            $lastTake = [Math]::Min([int64]$SampleBytes, $File.Length - $take)
            [void]$stream.Seek(-$lastTake, [System.IO.SeekOrigin]::End)
            $last = New-Object byte[] $lastTake
            [void]$stream.Read($last, 0, $last.Length)
        }

        $lenBytes = [BitConverter]::GetBytes([int64]$File.Length)
        $combined = New-Object byte[] ($first.Length + $last.Length + $lenBytes.Length)
        [Array]::Copy($first, 0, $combined, 0, $first.Length)
        if ($last.Length -gt 0) {
            [Array]::Copy($last, 0, $combined, $first.Length, $last.Length)
        }
        [Array]::Copy($lenBytes, 0, $combined, $first.Length + $last.Length, $lenBytes.Length)
        return Get-HexHash -Bytes $combined
    } finally {
        $stream.Dispose()
    }
}

function Get-FullHash {
    param([Parameter(Mandatory = $true)][System.IO.FileInfo]$File)
    $stream = [System.IO.File]::Open($File.FullName, [System.IO.FileMode]::Open, [System.IO.FileAccess]::Read, [System.IO.FileShare]::ReadWrite)
    $sha = [System.Security.Cryptography.SHA256]::Create()
    try {
        return ([BitConverter]::ToString($sha.ComputeHash($stream))).Replace("-", "").ToLowerInvariant()
    } finally {
        $sha.Dispose()
        $stream.Dispose()
    }
}

function Wait-JobTerminal {
    param(
        [string]$JobID,
        [int]$TimeoutSeconds = 3600
    )
    $uri = "$BaseURL/api/v1/batch/$JobID?include_data=true"
    for ($i = 0; $i -lt $TimeoutSeconds; $i++) {
        Start-Sleep -Seconds 1
        try {
            $job = Invoke-RestMethod -Method Get -Uri $uri -TimeoutSec 15
            if ($job.status -in @("completed", "failed", "cancelled")) {
                return $job
            }
        } catch {
            if ($i -gt 10) { throw }
        }
    }
    throw "batch job $JobID timed out"
}

Write-Host "Scanning media files..."
$allFiles = @(Get-ChildItem -LiteralPath $root -File -Recurse |
    Where-Object { $extensions -contains $_.Extension.ToLowerInvariant() } |
    Sort-Object FullName)

if ($allFiles.Count -eq 0) { throw "No media files found under $root" }

Write-Host ("FILES_TOTAL={0}" -f $allFiles.Count)

$duplicateRecords = New-Object System.Collections.Generic.List[object]
$duplicatePaths = New-Object 'System.Collections.Generic.HashSet[string]' ([System.StringComparer]::OrdinalIgnoreCase)

if (-not $SkipDuplicateCheck) {
    Write-Host "Duplicate phase 1/3: grouping by file size..."
    $sizeGroups = @($allFiles | Group-Object Length | Where-Object { $_.Count -gt 1 })

    Write-Host ("DUPLICATE_SIZE_GROUPS={0}" -f $sizeGroups.Count)
    $quickCandidates = New-Object System.Collections.Generic.List[object]

    foreach ($group in $sizeGroups) {
        foreach ($file in $group.Group) {
            $qh = Get-QuickHash -File $file -SampleBytes $QuickHashBytes
            $quickCandidates.Add([pscustomobject]@{
                File = $file
                QuickHash = $qh
            })
        }
    }

    Write-Host "Duplicate phase 2/3: grouping by quick hash..."
    $quickGroups = @($quickCandidates | Group-Object QuickHash | Where-Object { $_.Count -gt 1 })

    Write-Host ("DUPLICATE_QUICK_HASH_GROUPS={0}" -f $quickGroups.Count)
    $fullCandidates = New-Object System.Collections.Generic.List[object]

    foreach ($group in $quickGroups) {
        foreach ($item in $group.Group) {
            $fh = Get-FullHash -File $item.File
            $fullCandidates.Add([pscustomobject]@{
                File = $item.File
                FullHash = $fh
            })
        }
    }

    Write-Host "Duplicate phase 3/3: confirming full SHA-256..."
    $fullGroups = @($fullCandidates | Group-Object FullHash | Where-Object { $_.Count -gt 1 })

    foreach ($group in $fullGroups) {
        $ordered = @($group.Group | Sort-Object { $_.File.FullName })
        $canonical = $ordered[0].File.FullName
        for ($i = 1; $i -lt $ordered.Count; $i++) {
            $dup = $ordered[$i].File
            [void]$duplicatePaths.Add($dup.FullName)
            $duplicateRecords.Add([pscustomobject]@{
                duplicate_path = $dup.FullName
                canonical_path = $canonical
                bytes = [int64]$dup.Length
                sha256 = $group.Name
            })
        }
    }
}

$duplicateCsv = Join-Path $out "duplicates.csv"
$duplicateJson = Join-Path $out "duplicates.json"
$duplicateRecords | Export-Csv -LiteralPath $duplicateCsv -NoTypeInformation -Encoding utf8
$duplicateRecords | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $duplicateJson -Encoding utf8

$uniqueFiles = @($allFiles | Where-Object { -not $duplicatePaths.Contains($_.FullName) })
Write-Host ("DUPLICATES_CONFIRMED={0}" -f $duplicateRecords.Count)
Write-Host ("FILES_TO_JUDGE={0}" -f $uniqueFiles.Count)

try {
    $health = Invoke-RestMethod -Method Get -Uri "$BaseURL/health" -TimeoutSec 10
    if ($health.status -ne "ok") { throw "Health endpoint did not return ok" }
} catch {
    throw "Javinizer API is not reachable at $BaseURL : $($_.Exception.Message)"
}

$chunks = New-Object System.Collections.Generic.List[object]
for ($i = 0; $i -lt $uniqueFiles.Count; $i += $BatchSize) {
    $end = [Math]::Min($i + $BatchSize - 1, $uniqueFiles.Count - 1)
    $chunks.Add(@($uniqueFiles[$i..$end]))
}

Write-Host ("BATCHES_TOTAL={0} BATCH_SIZE={1} MAX_INFLIGHT={2}" -f $chunks.Count, $BatchSize, $MaxInflightBatches)

$pending = New-Object System.Collections.Generic.Queue[object]
foreach ($chunk in $chunks) { $pending.Enqueue($chunk) }
$inflight = @{}
$completedJobs = New-Object System.Collections.Generic.List[object]
$startedAt = Get-Date

while ($pending.Count -gt 0 -or $inflight.Count -gt 0) {
    while ($pending.Count -gt 0 -and $inflight.Count -lt $MaxInflightBatches) {
        $chunk = $pending.Dequeue()
        $paths = @($chunk | ForEach-Object { $_.FullName })
        $payload = @{
            files = $paths
            force = $true
            selected_scrapers = @("r18dev")
            operation_mode = "preview"
        } | ConvertTo-Json -Depth 8 -Compress

        $batch = Invoke-RestMethod -Method Post -Uri "$BaseURL/api/v1/batch/scrape" -ContentType "application/json" -Body $payload -TimeoutSec 60
        $jobID = [string]$batch.job_id
        if ([string]::IsNullOrWhiteSpace($jobID)) { throw "No job_id returned" }
        $inflight[$jobID] = [pscustomobject]@{
            job_id = $jobID
            paths = $paths
            submitted_at = Get-Date
        }
        Write-Host ("SUBMITTED job={0} files={1} inflight={2}" -f $jobID, $paths.Count, $inflight.Count)
    }

    Start-Sleep -Seconds 1

    foreach ($jobID in @($inflight.Keys)) {
        try {
            $job = Invoke-RestMethod -Method Get -Uri "$BaseURL/api/v1/batch/$jobID?include_data=true" -TimeoutSec 15
        } catch {
            continue
        }
        if ($job.status -in @("completed", "failed", "cancelled")) {
            $meta = $inflight[$jobID]
            $completedJobs.Add([pscustomobject]@{
                job_id = $jobID
                meta = $meta
                job = $job
            })
            $inflight.Remove($jobID)
            Write-Host ("FINISHED job={0} status={1} remaining={2}" -f $jobID, $job.status, ($pending.Count + $inflight.Count))
        }
    }
}

$records = New-Object System.Collections.Generic.List[object]

foreach ($entry in $completedJobs) {
    $job = $entry.job
    foreach ($path in $entry.meta.paths) {
        $value = $null
        if ($null -ne $job.results) {
            $prop = $job.results.PSObject.Properties | Where-Object { $_.Name -eq $path } | Select-Object -First 1
            if ($null -ne $prop) { $value = $prop.Value }
        }

        $movieID = ""
        $movieTitle = ""
        if ($null -ne $value) {
            $movieID = [string]$value.movie_id
            if ($null -ne $value.movie) { $movieTitle = [string]$value.movie.title }
        }

        $records.Add([pscustomobject]@{
            file = $path
            filename = [System.IO.Path]::GetFileName($path)
            job_id = $entry.job_id
            job_status = [string]$job.status
            movie_id = $movieID
            movie_title = $movieTitle
            resolved = (-not [string]::IsNullOrWhiteSpace($movieID))
        })
    }
}

$elapsed = (Get-Date) - $startedAt
$resolved = @($records | Where-Object { $_.resolved }).Count
$unresolved = $records.Count - $resolved

$resultCsv = Join-Path $out "bulk-results.csv"
$resultJson = Join-Path $out "bulk-results.json"
$summaryPath = Join-Path $out "summary.txt"

$records | Export-Csv -LiteralPath $resultCsv -NoTypeInformation -Encoding utf8
$records | ConvertTo-Json -Depth 12 | Set-Content -LiteralPath $resultJson -Encoding utf8

$rate = if ($elapsed.TotalSeconds -gt 0) { [Math]::Round($records.Count / $elapsed.TotalSeconds, 3) } else { 0 }

@(
    "status=PASS",
    "input_root=$root",
    "files_total=$($allFiles.Count)",
    "duplicates_confirmed=$($duplicateRecords.Count)",
    "files_judged=$($records.Count)",
    "resolved=$resolved",
    "unresolved=$unresolved",
    "batch_size=$BatchSize",
    "max_inflight_batches=$MaxInflightBatches",
    "elapsed_seconds=$([Math]::Round($elapsed.TotalSeconds, 2))",
    "files_per_second=$rate",
    "results_csv=$resultCsv",
    "duplicates_csv=$duplicateCsv"
) | Set-Content -LiteralPath $summaryPath -Encoding utf8

Get-Content $summaryPath
