param(
    [Parameter(Mandatory = $true)]
    [string]$ExePath,

    [string]$EvidenceDir = "evidence"
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$exe = (Resolve-Path $ExePath).Path
$evidence = [System.IO.Path]::GetFullPath((Join-Path $PWD $EvidenceDir))
New-Item -ItemType Directory -Path $evidence -Force | Out-Null

$titleBase = "一ヶ月間の禁欲の果てに彼女のルームメイト2人と浮気SEXだけに没頭した彼女不在の3日間"
$keepwordSuffix = "SPECIAL_4K_8K_VR_AI_字幕_中文字幕_-UC_UNCENSORED"
$titleWithKeepWords = "${titleBase}_${keepwordSuffix}"
$expectedID = "SSIS-001"
$configuredKeepWords = @("SPECIAL", "4K", "8K", "VR", "AI", "字幕", "中文字幕", "-UC", "UNCENSORED")

$inputDir = Join-Path $evidence "blackbox-input"
New-Item -ItemType Directory -Path $inputDir -Force | Out-Null
$filePath = Join-Path $inputDir ($titleWithKeepWords + ".mp4")
[System.IO.File]::WriteAllBytes($filePath, [byte[]](0x00, 0x01, 0x02, 0x03))

$configSource = (Resolve-Path "configs/config.yaml.example").Path

function To-YamlPath([string]$path) {
    return [System.IO.Path]::GetFullPath($path).Replace("\", "/")
}

function New-BlackboxConfig {
    param(
        [Parameter(Mandatory = $true)][string]$Destination,
        [Parameter(Mandatory = $true)][string]$LogPath,
        [Parameter(Mandatory = $true)][string]$DatabasePath,
        [Parameter(Mandatory = $true)][string]$DumpPath
    )

    New-Item -ItemType Directory -Path (Split-Path $Destination -Parent) -Force | Out-Null
    $yaml = Get-Content $configSource -Raw
    $allowed = To-YamlPath $inputDir
    $log = To-YamlPath $LogPath
    $db = To-YamlPath $DatabasePath
    $dump = To-YamlPath $DumpPath

    $oldAllowed = "        allowed_directories: []"
    $newAllowed = "        allowed_directories:`r`n            - `"$allowed`""
    $yaml = $yaml.Replace($oldAllowed, $newAllowed)

    $oldFormat = "    file_format: <ID><IF:MULTIPART>-pt<PART></IF>"
    $newFormat = "    file_format: <ID><KEEPWORDS:SPECIAL|4K|8K|VR|AI|字幕|中文字幕|-UC|UNCENSORED;PREFIX= - ;DELIM= ><IF:MULTIPART>-pt<PART></IF>"
    $yaml = $yaml.Replace($oldFormat, $newFormat)

    $yaml = $yaml.Replace("        path: data/r18dev/r18dev_dump.db", "        path: `"$dump`"")
    $yaml = $yaml.Replace("    dsn: data/javinizer.db", "    dsn: `"$db`"")
    $yaml = $yaml.Replace('    output: "stdout,data/logs/javinizer.log"', "    output: `"stderr,$log`"")

    if ($yaml.Contains($oldAllowed)) { throw "allowed_directories replacement did not apply" }
    if ($yaml.Contains($oldFormat)) { throw "KEEPWORDS file_format replacement did not apply" }
    if ($yaml.Contains("    dsn: data/javinizer.db")) { throw "database path replacement did not apply" }
    if ($yaml.Contains('    output: "stdout,data/logs/javinizer.log"')) { throw "logging output replacement did not apply" }

    Set-Content -Path $Destination -Value $yaml -Encoding utf8
}

function Read-TextIfExists([string]$path) {
    if (Test-Path $path) {
        return Get-Content $path -Raw
    }
    return ""
}

function Assert-WebLookupEvidence {
    param(
        [Parameter(Mandatory = $true)][string]$Combined,
        [Parameter(Mandatory = $true)][string]$GateName,
        [Parameter(Mandatory = $true)][string]$ResultPath
    )

    $lines = $Combined -split "`r?`n"
    $cleanupLine = ($lines | Where-Object { $_ -match 'web-title cleanup ' } | Select-Object -Last 1)
    $lookupLine = ($lines | Where-Object { $_ -match 'web-title lookup input=' } | Select-Object -Last 1)
    $resolvedLine = ($lines | Where-Object { $_ -match 'title web lookup resolved .* -> SSIS-001' } | Select-Object -Last 1)

    "cleanup_line=$cleanupLine" | Out-File $ResultPath -Encoding utf8 -Append
    "lookup_line=$lookupLine" | Out-File $ResultPath -Encoding utf8 -Append
    "resolved_line=$resolvedLine" | Out-File $ResultPath -Encoding utf8 -Append

    if ([string]::IsNullOrWhiteSpace($cleanupLine)) {
        throw "$GateName did not emit web-title cleanup evidence"
    }
    if ([string]::IsNullOrWhiteSpace($lookupLine)) {
        throw "$GateName did not emit web-title lookup evidence"
    }
    if ([string]::IsNullOrWhiteSpace($resolvedLine)) {
        throw "$GateName did not resolve the title to $expectedID"
    }

    foreach ($unwanted in $configuredKeepWords) {
        if ($lookupLine.IndexOf($unwanted, [System.StringComparison]::OrdinalIgnoreCase) -ge 0) {
            throw "$GateName leaked KEEPWORD '$unwanted' into cleaned web lookup: $lookupLine"
        }
    }

    if ($lookupLine.IndexOf($titleBase, [System.StringComparison]::Ordinal) -lt 0) {
        throw "$GateName lost the real title before web lookup: $lookupLine"
    }
    if ($Combined -match 'No results from any scraper') {
        throw "$GateName reproduced the original 'No results from any scraper' failure"
    }
}

$requirementsPath = Join-Path $evidence "blackbox-requirements.txt"
@(
    "requirements_version=2",
    "exact_exe=$exe",
    "expected_catalog_id=$expectedID",
    "source_title=$titleBase",
    "filename_keepwords=$keepwordSuffix",
    "gate_a=exact built EXE CLI title -> KEEPWORDS strip -> live web -> r18dev -> success",
    "gate_b=exact built EXE web server -> auth -> batch file path -> worker -> KEEPWORDS strip -> live web -> r18dev -> completed job",
    "distribution_rule=both gate_a and gate_b must PASS"
) | Out-File $requirementsPath -Encoding utf8

# -----------------------------------------------------------------------------
# Gate A: exact built Windows GUI EXE invoked through its CLI scrape command.
# Start-Process -Wait is required because PowerShell '&' does not reliably wait
# for a windowsgui subsystem executable.
# -----------------------------------------------------------------------------
$cliDir = Join-Path $evidence "blackbox-cli"
New-Item -ItemType Directory -Path $cliDir -Force | Out-Null
$cliConfig = Join-Path $cliDir "config.yaml"
$cliAppLog = Join-Path $cliDir "app.log"
$cliDB = Join-Path $cliDir "javinizer.db"
$cliDump = Join-Path $cliDir "r18dev_dump.db"
$cliStdout = Join-Path $cliDir "stdout.txt"
$cliStderr = Join-Path $cliDir "stderr.txt"
$cliResult = Join-Path $cliDir "result.txt"
New-BlackboxConfig -Destination $cliConfig -LogPath $cliAppLog -DatabasePath $cliDB -DumpPath $cliDump

$cliArgs = @("--config", $cliConfig, "--verbose", "scrape", $titleWithKeepWords, "--scrapers", "r18dev")
"gate=CLI_EXACT_EXE" | Out-File $cliResult -Encoding utf8
"command=Javinizer-KEEPWORDS.exe --config <cli-config> --verbose scrape <known-title_KEEPWORDS-matrix> --scrapers r18dev" | Out-File $cliResult -Encoding utf8 -Append
"expected_catalog_id=$expectedID" | Out-File $cliResult -Encoding utf8 -Append

$cliProcess = Start-Process -FilePath $exe -ArgumentList $cliArgs -Wait -PassThru -RedirectStandardOutput $cliStdout -RedirectStandardError $cliStderr
"exit_code=$($cliProcess.ExitCode)" | Out-File $cliResult -Encoding utf8 -Append
if ($cliProcess.ExitCode -ne 0) {
    throw "Gate A: exact built EXE CLI exited with $($cliProcess.ExitCode)"
}

$cliCombined = (Read-TextIfExists $cliStdout) + "`n" + (Read-TextIfExists $cliStderr) + "`n" + (Read-TextIfExists $cliAppLog)
Assert-WebLookupEvidence -Combined $cliCombined -GateName "Gate A" -ResultPath $cliResult
if ($cliCombined -notmatch 'SSIS-001') {
    throw "Gate A: output/log evidence does not contain $expectedID"
}
"gate_a_result=PASS" | Out-File $cliResult -Encoding utf8 -Append

# -----------------------------------------------------------------------------
# Gate B: exact same built EXE runs the real localhost API server. A real dummy
# video filename (no manual_inputs override) is submitted to /batch/scrape.
# This exercises the filename -> worker -> scrape -> live web -> r18dev path.
# -----------------------------------------------------------------------------
$apiDir = Join-Path $evidence "blackbox-api"
New-Item -ItemType Directory -Path $apiDir -Force | Out-Null
$apiConfig = Join-Path $apiDir "config.yaml"
$apiAppLog = Join-Path $apiDir "app.log"
$apiDB = Join-Path $apiDir "javinizer.db"
$apiDump = Join-Path $apiDir "r18dev_dump.db"
$apiStdout = Join-Path $apiDir "server-stdout.txt"
$apiStderr = Join-Path $apiDir "server-stderr.txt"
$apiResult = Join-Path $apiDir "result.txt"
New-BlackboxConfig -Destination $apiConfig -LogPath $apiAppLog -DatabasePath $apiDB -DumpPath $apiDump

$listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
$listener.Start()
$port = ([System.Net.IPEndPoint]$listener.LocalEndpoint).Port
$listener.Stop()
$baseURL = "http://127.0.0.1:$port"

"gate=API_FILENAME_EXACT_EXE" | Out-File $apiResult -Encoding utf8
"server_url=$baseURL" | Out-File $apiResult -Encoding utf8 -Append
"input_file=$filePath" | Out-File $apiResult -Encoding utf8 -Append
"expected_catalog_id=$expectedID" | Out-File $apiResult -Encoding utf8 -Append

$serverArgs = @("--config", $apiConfig, "--verbose", "web", "--host", "127.0.0.1", "--port", "$port")
$server = Start-Process -FilePath $exe -ArgumentList $serverArgs -PassThru -RedirectStandardOutput $apiStdout -RedirectStandardError $apiStderr
"server_pid=$($server.Id)" | Out-File $apiResult -Encoding utf8 -Append

try {
    $ready = $false
    for ($i = 0; $i -lt 60; $i++) {
        Start-Sleep -Milliseconds 500
        $server.Refresh()
        if ($server.HasExited) {
            throw "Gate B: API server exited before health check; exit=$($server.ExitCode)"
        }
        try {
            $health = Invoke-RestMethod -Method Get -Uri "$baseURL/health" -TimeoutSec 5
            if ($null -ne $health -and $health.status -eq "ok") {
                $health | ConvertTo-Json -Depth 10 | Out-File (Join-Path $apiDir "health.json") -Encoding utf8
                $ready = $true
                break
            }
        } catch {
            # Server may still be starting.
        }
    }
    if (-not $ready) {
        throw "Gate B: API server did not become healthy"
    }

    $setupBody = @{
        username = "proof-admin"
        password = "proof-password-123"
        remember_me = $true
    } | ConvertTo-Json -Compress
    $setupBody | Out-File (Join-Path $apiDir "auth-setup-request.json") -Encoding utf8
    $setup = Invoke-RestMethod -Method Post -Uri "$baseURL/api/v1/auth/setup" -ContentType "application/json" -Body $setupBody -TimeoutSec 15
    $setup | ConvertTo-Json -Depth 10 | Out-File (Join-Path $apiDir "auth-setup-response.json") -Encoding utf8
    if (-not $setup.authenticated -or [string]::IsNullOrWhiteSpace([string]$setup.session_id)) {
        throw "Gate B: localhost auth setup did not produce an authenticated session"
    }

    $headers = @{ "X-Session-ID" = [string]$setup.session_id }
    $batchPayloadObject = @{
        files = @($filePath)
        force = $true
        selected_scrapers = @("r18dev")
        operation_mode = "preview"
    }
    $batchBody = $batchPayloadObject | ConvertTo-Json -Depth 10 -Compress
    $batchBody | Out-File (Join-Path $apiDir "batch-scrape-request.json") -Encoding utf8
    $batch = Invoke-RestMethod -Method Post -Uri "$baseURL/api/v1/batch/scrape" -Headers $headers -ContentType "application/json" -Body $batchBody -TimeoutSec 30
    $batch | ConvertTo-Json -Depth 10 | Out-File (Join-Path $apiDir "batch-scrape-response.json") -Encoding utf8
    $jobID = [string]$batch.job_id
    if ([string]::IsNullOrWhiteSpace($jobID)) {
        throw "Gate B: batch scrape did not return job_id"
    }
    "job_id=$jobID" | Out-File $apiResult -Encoding utf8 -Append

    $jobUri = '{0}/api/v1/batch/{1}?include_data=true' -f $baseURL, $jobID
    "poll_uri=$jobUri" | Out-File $apiResult -Encoding utf8 -Append

    $job = $null
    $terminal = $false
    for ($i = 0; $i -lt 180; $i++) {
        Start-Sleep -Seconds 1
        $job = Invoke-RestMethod -Method Get -Uri $jobUri -Headers $headers -TimeoutSec 15
        if ($job.status -in @("completed", "failed", "cancelled")) {
            $terminal = $true
            break
        }
    }
    if ($null -ne $job) {
        $job | ConvertTo-Json -Depth 100 | Out-File (Join-Path $apiDir "batch-job-final.json") -Encoding utf8
    }
    if (-not $terminal) {
        throw "Gate B: batch job did not reach a terminal state"
    }
    "job_status=$($job.status)" | Out-File $apiResult -Encoding utf8 -Append
    "job_completed=$($job.completed)" | Out-File $apiResult -Encoding utf8 -Append
    "job_failed=$($job.failed)" | Out-File $apiResult -Encoding utf8 -Append
    if ($job.status -ne "completed" -or [int]$job.failed -ne 0) {
        throw "Gate B: batch job was not successful (status=$($job.status), failed=$($job.failed))"
    }

    $resultProperty = $job.results.PSObject.Properties | Where-Object { $_.Name -eq $filePath } | Select-Object -First 1
    if ($null -eq $resultProperty) {
        throw "Gate B: final job has no result keyed by submitted file path"
    }
    $fileResult = $resultProperty.Value
    $fileResult | ConvertTo-Json -Depth 100 | Out-File (Join-Path $apiDir "file-result.json") -Encoding utf8
    "file_result_status=$($fileResult.status)" | Out-File $apiResult -Encoding utf8 -Append
    "file_result_movie_id=$($fileResult.movie_id)" | Out-File $apiResult -Encoding utf8 -Append
    if ([string]$fileResult.movie_id -ne $expectedID) {
        throw "Gate B: file result movie_id=$($fileResult.movie_id), want $expectedID"
    }
    if ($null -eq $fileResult.movie -or [string]$fileResult.movie.id -ne $expectedID) {
        throw "Gate B: file result movie.id did not equal $expectedID"
    }

    $apiCombined = (Read-TextIfExists $apiStdout) + "`n" + (Read-TextIfExists $apiStderr) + "`n" + (Read-TextIfExists $apiAppLog)
    Assert-WebLookupEvidence -Combined $apiCombined -GateName "Gate B" -ResultPath $apiResult
    "gate_b_result=PASS" | Out-File $apiResult -Encoding utf8 -Append
} finally {
    if ($null -ne $server) {
        try {
            $server.Refresh()
            if (-not $server.HasExited) {
                Stop-Process -Id $server.Id -Force
                $server.WaitForExit(10000)
            }
        } catch {
            "server_cleanup_error=$($_.Exception.Message)" | Out-File $apiResult -Encoding utf8 -Append
        }
    }
}

@(
    "blackbox_verification=PASS",
    "gate_a=PASS",
    "gate_b=PASS",
    "expected_catalog_id=$expectedID",
    "tested_exe_sha256=$((Get-FileHash $exe -Algorithm SHA256).Hash.ToLowerInvariant())"
) | Out-File (Join-Path $evidence "blackbox-summary.txt") -Encoding utf8

Get-Content $cliResult
Get-Content $apiResult
Get-Content (Join-Path $evidence "blackbox-summary.txt")