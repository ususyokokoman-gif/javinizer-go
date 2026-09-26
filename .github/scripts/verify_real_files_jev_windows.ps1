param(
    [Parameter(Mandatory = $true)]
    [string]$ExePath,

    [Parameter(Mandatory = $true)]
    [string]$InputRoot,

    [string]$ConfigPath = "configs/config.yaml.example",

    [string]$EvidenceDir = "evidence/real-files-jev",

    [int]$MaxFiles = 20,

    [int]$MinTitleResolvedFiles = 5,

    [int]$MinAcceptedFiles = 1
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

if ([string]::IsNullOrWhiteSpace($env:TYPESAFE_API_KEY)) {
    throw "TYPESAFE_API_KEY is required for real-file Jev verification"
}
$env:JAVINIZER_JEV_CATALOG_THRESHOLD = "0.80"

$exe = (Resolve-Path $ExePath).Path
$root = (Resolve-Path $InputRoot).Path
$configSource = (Resolve-Path $ConfigPath).Path
$evidence = [System.IO.Path]::GetFullPath((Join-Path $PWD $EvidenceDir))

if ($MaxFiles -lt 1) { throw "MaxFiles must be >= 1" }
if ($MinTitleResolvedFiles -lt 1) { throw "MinTitleResolvedFiles must be >= 1" }
if ($MinAcceptedFiles -lt 1) { throw "MinAcceptedFiles must be >= 1" }

New-Item -ItemType Directory -Path $evidence -Force | Out-Null

$extensions = @(".mp4", ".mkv", ".avi", ".wmv", ".flv", ".mov", ".m4v")
$files = Get-ChildItem -LiteralPath $root -File -Recurse |
    Where-Object { $extensions -contains $_.Extension.ToLowerInvariant() } |
    Sort-Object FullName |
    Select-Object -First $MaxFiles

if (@($files).Count -eq 0) {
    throw "No media files found under $root"
}

function To-YamlPath([string]$path) {
    return [System.IO.Path]::GetFullPath($path).Replace("\", "/")
}

function Read-TextIfExists([string]$path) {
    if (Test-Path $path) { return Get-Content $path -Raw }
    return ""
}

function New-VerificationConfig {
    param(
        [Parameter(Mandatory = $true)][string]$Destination,
        [Parameter(Mandatory = $true)][string]$LogPath,
        [Parameter(Mandatory = $true)][string]$DatabasePath,
        [Parameter(Mandatory = $true)][string]$DumpPath
    )

    $yaml = Get-Content $configSource -Raw
    $allowed = To-YamlPath $root
    $log = To-YamlPath $LogPath
    $db = To-YamlPath $DatabasePath
    $dump = To-YamlPath $DumpPath

    $yaml = [regex]::Replace(
        $yaml,
        '(?m)^        allowed_directories:\s*\[\]\s*$',
        "        allowed_directories:`r`n            - `"$allowed`"",
        1
    )
    $yaml = [regex]::Replace(
        $yaml,
        '(?m)^        path:\s*data/r18dev/r18dev_dump\.db\s*$',
        "        path: `"$dump`"",
        1
    )
    $yaml = [regex]::Replace(
        $yaml,
        '(?m)^    dsn:\s*data/javinizer\.db\s*$',
        "    dsn: `"$db`"",
        1
    )
    $yaml = [regex]::Replace(
        $yaml,
        '(?m)^    output:\s*"stdout,data/logs/javinizer\.log"\s*$',
        "    output: `"stderr,$log`"",
        1
    )

    # Force the product behavior under test regardless of whether the source
    # example config was created before or after catalog_id_validation existed.
    if ($yaml -match '(?m)^    catalog_id_validation:\s*$') {
        $yaml = [regex]::Replace(
            $yaml,
            '(?ms)^    catalog_id_validation:\s*\r?\n(?:        .*\r?\n){1,8}?(?=    \S)',
            "    catalog_id_validation:`r`n        enabled: true`r`n        threshold: 0.80`r`n        model: jev-latest`r`n        endpoint: https://api.typesafe.ai/v1/systemone`r`n        api_key: `"`"`r`n",
            1
        )
    } else {
        $metadataMarker = "metadata:"
        $idx = $yaml.IndexOf($metadataMarker, [System.StringComparison]::Ordinal)
        if ($idx -lt 0) { throw "metadata block not found in config" }
        $insertAt = $idx + $metadataMarker.Length
        $block = "`r`n    catalog_id_validation:`r`n        enabled: true`r`n        threshold: 0.80`r`n        model: jev-latest`r`n        endpoint: https://api.typesafe.ai/v1/systemone`r`n        api_key: `"`""
        $yaml = $yaml.Insert($insertAt, $block)
    }

    Set-Content -LiteralPath $Destination -Value $yaml -Encoding utf8
}

function Wait-JobTerminal {
    param(
        [Parameter(Mandatory = $true)][string]$BaseURL,
        [Parameter(Mandatory = $true)][string]$JobID,
        [int]$TimeoutSeconds = 180
    )

    $uri = "$BaseURL/api/v1/batch/$JobID?include_data=true"
    for ($i = 0; $i -lt $TimeoutSeconds; $i++) {
        Start-Sleep -Seconds 1
        $job = Invoke-RestMethod -Method Get -Uri $uri -TimeoutSec 10
        if ($job.status -in @("completed", "failed", "cancelled")) {
            return $job
        }
    }
    throw "batch job $JobID did not reach terminal state"
}

$configPath = Join-Path $evidence "config.yaml"
$appLog = Join-Path $evidence "app.log"
$dbPath = Join-Path $evidence "javinizer.db"
$dumpPath = Join-Path $evidence "r18dev_dump.db"
$stdoutPath = Join-Path $evidence "server-stdout.txt"
$stderrPath = Join-Path $evidence "server-stderr.txt"
New-VerificationConfig -Destination $configPath -LogPath $appLog -DatabasePath $dbPath -DumpPath $dumpPath

$listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
$listener.Start()
$port = ([System.Net.IPEndPoint]$listener.LocalEndpoint).Port
$listener.Stop()
$baseURL = "http://127.0.0.1:$port"

$server = Start-Process -FilePath $exe -ArgumentList @(
    "--config", $configPath,
    "--verbose",
    "web",
    "--host", "127.0.0.1",
    "--port", "$port"
) -PassThru -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath

$records = @()
$acceptedCount = 0
$rejectedCount = 0
$titleResolvedCount = 0
$liveFailureCount = 0

try {
    $ready = $false
    for ($i = 0; $i -lt 60; $i++) {
        Start-Sleep -Milliseconds 500
        $server.Refresh()
        if ($server.HasExited) {
            throw "Javinizer server exited before health check; exit=$($server.ExitCode)"
        }
        try {
            $health = Invoke-RestMethod -Method Get -Uri "$baseURL/health" -TimeoutSec 5
            if ($health.status -eq "ok") {
                $ready = $true
                break
            }
        } catch {}
    }
    if (-not $ready) { throw "Javinizer server did not become healthy" }

    foreach ($file in $files) {
        $beforeLog = Read-TextIfExists $appLog
        $beforeLength = $beforeLog.Length

        $payload = @{
            files = @($file.FullName)
            force = $true
            selected_scrapers = @("r18dev")
            operation_mode = "preview"
        } | ConvertTo-Json -Depth 10 -Compress

        $batch = Invoke-RestMethod -Method Post -Uri "$baseURL/api/v1/batch/scrape" -ContentType "application/json" -Body $payload -TimeoutSec 30
        $jobID = [string]$batch.job_id
        if ([string]::IsNullOrWhiteSpace($jobID)) {
            throw "No job_id for $($file.FullName)"
        }
        $job = Wait-JobTerminal -BaseURL $baseURL -JobID $jobID

        $afterLog = Read-TextIfExists $appLog
        $delta = if ($afterLog.Length -ge $beforeLength) { $afterLog.Substring($beforeLength) } else { $afterLog }
        $deltaPath = Join-Path $evidence ("log-" + ([guid]::NewGuid().ToString("N")) + ".txt")
        Set-Content -LiteralPath $deltaPath -Value $delta -Encoding utf8

        $webMatches = [regex]::Matches($delta, 'web search provider=(?<provider>[^ ]+) query="(?<query>.*?)" results=(?<results>[0-9]+)')
        $jevAccept = [regex]::Match($delta, 'Jev catalog gate accepted candidate=(?<candidate>[^ ]+) probability=(?<prob>[0-9.]+) threshold=(?<threshold>[0-9.]+) model=(?<model>[^\s]+)')
        $jevReject = [regex]::Match($delta, 'Jev catalog gate rejected candidate=(?<candidate>[^ ]+) probability=(?<prob>[0-9.]+) threshold=(?<threshold>[0-9.]+) model=(?<model>[^\s]+)')
        $jevFailure = [regex]::Match($delta, 'Jev catalog gate failed closed for candidate=(?<candidate>[^: ]+)')

        $provider = ""
        $query = ""
        $resultCount = 0
        if ($webMatches.Count -gt 0) {
            $lastWeb = $webMatches[$webMatches.Count - 1]
            $provider = $lastWeb.Groups["provider"].Value
            $query = $lastWeb.Groups["query"].Value
            $resultCount = [int]$lastWeb.Groups["results"].Value
        }

        $decision = "none"
        $candidate = ""
        $probability = $null
        $threshold = 0.80
        $model = ""
        if ($jevAccept.Success) {
            $decision = "accepted"
            $candidate = $jevAccept.Groups["candidate"].Value
            $probability = [double]$jevAccept.Groups["prob"].Value
            $threshold = [double]$jevAccept.Groups["threshold"].Value
            $model = $jevAccept.Groups["model"].Value
            $acceptedCount++
        } elseif ($jevReject.Success) {
            $decision = "rejected"
            $candidate = $jevReject.Groups["candidate"].Value
            $probability = [double]$jevReject.Groups["prob"].Value
            $threshold = [double]$jevReject.Groups["threshold"].Value
            $model = $jevReject.Groups["model"].Value
            $rejectedCount++
        } elseif ($jevFailure.Success) {
            $decision = "jev_error"
            $candidate = $jevFailure.Groups["candidate"].Value
            $liveFailureCount++
        }

        $fileResult = $null
        if ($null -ne $job.results) {
            $prop = $job.results.PSObject.Properties | Where-Object { $_.Name -eq $file.FullName } | Select-Object -First 1
            if ($null -ne $prop) { $fileResult = $prop.Value }
        }
        $movieID = if ($null -ne $fileResult) { [string]$fileResult.movie_id } else { "" }
        $movieTitle = if ($null -ne $fileResult -and $null -ne $fileResult.movie) { [string]$fileResult.movie.title } else { "" }

        if ($webMatches.Count -gt 0) { $titleResolvedCount++ }

        if ($decision -eq "accepted") {
            if ($probability -lt 0.80) {
                throw "accepted candidate below threshold for $($file.Name): $probability"
            }
            if ($job.status -ne "completed" -or [int]$job.failed -ne 0) {
                throw "accepted candidate did not complete successfully for $($file.Name)"
            }
            if ([string]::IsNullOrWhiteSpace($movieID) -or $movieID -ne $candidate) {
                throw "accepted candidate/final movie_id mismatch for $($file.Name): candidate=$candidate movie_id=$movieID"
            }
        } elseif ($decision -eq "rejected") {
            if ($probability -ge 0.80) {
                throw "rejected candidate at/above threshold for $($file.Name): $probability"
            }
            if (-not [string]::IsNullOrWhiteSpace($movieID) -and $movieID -eq $candidate) {
                throw "rejected candidate was still adopted for $($file.Name): $candidate"
            }
        } elseif ($decision -eq "jev_error") {
            throw "live Jev validation failed closed for $($file.Name)"
        }

        $records += [pscustomobject]@{
            file = $file.FullName
            filename = $file.Name
            job_status = [string]$job.status
            web_provider = $provider
            web_results = $resultCount
            web_query = $query
            jev_decision = $decision
            candidate_id = $candidate
            jev_probability = $probability
            jev_threshold = $threshold
            jev_model = $model
            final_movie_id = $movieID
            final_movie_title = $movieTitle
            log_evidence = $deltaPath
        }
    }
} finally {
    try {
        $server.Refresh()
        if (-not $server.HasExited) {
            Stop-Process -Id $server.Id -Force
            $server.WaitForExit(10000)
        }
    } catch {}
}

$jsonPath = Join-Path $evidence "real-file-results.json"
$csvPath = Join-Path $evidence "real-file-results.csv"
$summaryPath = Join-Path $evidence "summary.txt"

$records | ConvertTo-Json -Depth 20 | Set-Content -LiteralPath $jsonPath -Encoding utf8
$records | Export-Csv -LiteralPath $csvPath -NoTypeInformation -Encoding utf8

@(
    "status=INCOMPLETE",
    "exact_exe=$exe",
    "tested_exe_sha256=$((Get-FileHash $exe -Algorithm SHA256).Hash.ToLowerInvariant())",
    "input_root=$root",
    "files_sampled=$(@($files).Count)",
    "title_resolved_files=$titleResolvedCount",
    "jev_accepted=$acceptedCount",
    "jev_rejected=$rejectedCount",
    "jev_live_failures=$liveFailureCount",
    "threshold=0.80"
) | Set-Content -LiteralPath $summaryPath -Encoding utf8

if ($titleResolvedCount -lt $MinTitleResolvedFiles) {
    throw "Only $titleResolvedCount files exercised title->Web resolution; need at least $MinTitleResolvedFiles"
}
if ($acceptedCount -lt $MinAcceptedFiles) {
    throw "Only $acceptedCount files were safely accepted by Jev; need at least $MinAcceptedFiles"
}
if ($liveFailureCount -ne 0) {
    throw "$liveFailureCount Jev live API failures occurred"
}
if (($acceptedCount + $rejectedCount) -lt $titleResolvedCount) {
    throw "Some title-resolved files did not produce a Jev decision"
}

(Get-Content $summaryPath) -replace '^status=INCOMPLETE$', 'status=PASS' | Set-Content -LiteralPath $summaryPath -Encoding utf8
Get-Content $summaryPath
