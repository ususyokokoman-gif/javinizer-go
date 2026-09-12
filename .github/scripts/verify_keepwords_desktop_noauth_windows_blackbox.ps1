param(
    [Parameter(Mandatory = $true)]
    [string]$ExePath,

    [string]$EvidenceDir = "evidence-noauth"
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
    if (Test-Path $path) { return Get-Content $path -Raw }
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

    if ([string]::IsNullOrWhiteSpace($cleanupLine)) { throw "$GateName did not emit web-title cleanup evidence" }
    if ([string]::IsNullOrWhiteSpace($lookupLine)) { throw "$GateName did not emit web-title lookup evidence" }
    if ([string]::IsNullOrWhiteSpace($resolvedLine)) { throw "$GateName did not resolve the title to $expectedID" }

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

@(
    "requirements_version=3",
    "exact_exe=$exe",
    "expected_catalog_id=$expectedID",
    "source_title=$titleBase",
    "filename_keepwords=$keepwordSuffix",
    "gate_a=exact built EXE CLI title -> KEEPWORDS strip -> live web -> r18dev -> success",
    "gate_b=exact built desktop EXE localhost API with NO credentials/session -> auth status local -> batch filename -> worker -> KEEPWORDS strip -> live web -> r18dev -> completed",
    "gate_c=exact built desktop EXE served UI -> main navigation visible -> login fields absent -> logout absent -> no setup/login POST",
    "auth_files_must_remain_absent=true",
    "distribution_rule=gate_a + gate_b + gate_c + tested EXE SHA identity must PASS"
) | Out-File (Join-Path $evidence "blackbox-requirements.txt") -Encoding utf8

# Gate A: exact EXE CLI web-first behavior.
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
$cliProcess = Start-Process -FilePath $exe -ArgumentList $cliArgs -Wait -PassThru -RedirectStandardOutput $cliStdout -RedirectStandardError $cliStderr
"exit_code=$($cliProcess.ExitCode)" | Out-File $cliResult -Encoding utf8 -Append
if ($cliProcess.ExitCode -ne 0) { throw "Gate A: exact built EXE CLI exited with $($cliProcess.ExitCode)" }
$cliCombined = (Read-TextIfExists $cliStdout) + "`n" + (Read-TextIfExists $cliStderr) + "`n" + (Read-TextIfExists $cliAppLog)
Assert-WebLookupEvidence -Combined $cliCombined -GateName "Gate A" -ResultPath $cliResult
if ($cliCombined -notmatch 'SSIS-001') { throw "Gate A: evidence does not contain $expectedID" }
"gate_a_result=PASS" | Out-File $cliResult -Encoding utf8 -Append

# Gate B/C: exact same EXE, localhost API and real embedded UI, no credentials.
$apiDir = Join-Path $evidence "blackbox-desktop-noauth"
New-Item -ItemType Directory -Path $apiDir -Force | Out-Null
$apiConfig = Join-Path $apiDir "config.yaml"
$apiAppLog = Join-Path $apiDir "app.log"
$apiDB = Join-Path $apiDir "javinizer.db"
$apiDump = Join-Path $apiDir "r18dev_dump.db"
$apiStdout = Join-Path $apiDir "server-stdout.txt"
$apiStderr = Join-Path $apiDir "server-stderr.txt"
$apiResult = Join-Path $apiDir "result.txt"
$credentialPath = Join-Path $apiDir "auth.credentials.json"
$sessionPath = Join-Path $apiDir "auth.sessions.json"
New-BlackboxConfig -Destination $apiConfig -LogPath $apiAppLog -DatabasePath $apiDB -DumpPath $apiDump
Remove-Item $credentialPath, $sessionPath -Force -ErrorAction SilentlyContinue
if ((Test-Path $credentialPath) -or (Test-Path $sessionPath)) { throw "Gate B: auth files existed before server start" }

$listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
$listener.Start()
$port = ([System.Net.IPEndPoint]$listener.LocalEndpoint).Port
$listener.Stop()
$baseURL = "http://127.0.0.1:$port"

@(
    "gate=DESKTOP_LOCALHOST_NOAUTH_EXACT_EXE",
    "server_url=$baseURL",
    "input_file=$filePath",
    "expected_catalog_id=$expectedID",
    "auth_setup_called=false",
    "auth_login_called=false",
    "session_header_used=false"
) | Out-File $apiResult -Encoding utf8

$serverArgs = @("--config", $apiConfig, "--verbose", "web", "--host", "127.0.0.1", "--port", "$port")
$server = Start-Process -FilePath $exe -ArgumentList $serverArgs -PassThru -RedirectStandardOutput $apiStdout -RedirectStandardError $apiStderr
"server_pid=$($server.Id)" | Out-File $apiResult -Encoding utf8 -Append

try {
    $ready = $false
    for ($i = 0; $i -lt 60; $i++) {
        Start-Sleep -Milliseconds 500
        $server.Refresh()
        if ($server.HasExited) { throw "Gate B: API server exited before health check; exit=$($server.ExitCode)" }
        try {
            $health = Invoke-RestMethod -Method Get -Uri "$baseURL/health" -TimeoutSec 5
            if ($null -ne $health -and $health.status -eq "ok") {
                $health | ConvertTo-Json -Depth 10 | Out-File (Join-Path $apiDir "health.json") -Encoding utf8
                $ready = $true
                break
            }
        } catch {}
    }
    if (-not $ready) { throw "Gate B: API server did not become healthy" }

    if ((Test-Path $credentialPath) -or (Test-Path $sessionPath)) {
        throw "Gate B: desktop startup created authentication state unexpectedly"
    }

    # No cookie jar, Authorization, X-Session-ID, setup, or login is used here.
    $authStatus = Invoke-RestMethod -Method Get -Uri "$baseURL/api/v1/auth/status" -TimeoutSec 15
    $authStatus | ConvertTo-Json -Depth 10 | Out-File (Join-Path $apiDir "auth-status-no-credentials.json") -Encoding utf8
    if (-not $authStatus.initialized -or -not $authStatus.authenticated -or [string]$authStatus.username -ne "local") {
        throw "Gate B: desktop localhost auth status is not automatic local authentication"
    }
    if (-not [string]::IsNullOrWhiteSpace([string]$authStatus.session_id)) {
        throw "Gate B: desktop localhost unexpectedly returned a session ID"
    }
    "auth_status=authenticated_local_without_session" | Out-File $apiResult -Encoding utf8 -Append

    # UI proof against the same running EXE.
    $oldURL = $env:JAVINIZER_BLACKBOX_URL
    $oldEvidence = $env:JAVINIZER_BLACKBOX_EVIDENCE_DIR
    $env:JAVINIZER_BLACKBOX_URL = $baseURL
    $env:JAVINIZER_BLACKBOX_EVIDENCE_DIR = $apiDir
    $uiLog = Join-Path $apiDir "playwright-noauth.txt"
    Push-Location "web/frontend"
    try {
        $uiOutput = & npx playwright test --config playwright.desktop-blackbox.config.ts 2>&1
        $uiExit = $LASTEXITCODE
        $uiOutput | Out-File $uiLog -Encoding utf8
        if ($uiExit -ne 0) { throw "Gate C: Playwright no-auth UI test failed with exit $uiExit" }
    } finally {
        Pop-Location
        $env:JAVINIZER_BLACKBOX_URL = $oldURL
        $env:JAVINIZER_BLACKBOX_EVIDENCE_DIR = $oldEvidence
    }
    $uiScreenshot = Join-Path $apiDir "desktop-noauth-main-ui.png"
    $uiAuthRequests = Join-Path $apiDir "desktop-noauth-auth-requests.txt"
    if (-not (Test-Path $uiScreenshot)) { throw "Gate C: UI screenshot evidence missing" }
    if (-not (Test-Path $uiAuthRequests)) { throw "Gate C: auth request evidence missing" }
    $authRequestText = Get-Content $uiAuthRequests -Raw
    if ($authRequestText -match '(?im)^POST /api/v1/auth/(setup|login)$') {
        throw "Gate C: UI attempted setup/login"
    }
    "gate_c_ui_result=PASS" | Out-File $apiResult -Encoding utf8 -Append

    $batchPayloadObject = @{
        files = @($filePath)
        force = $true
        selected_scrapers = @("r18dev")
        operation_mode = "preview"
    }
    $batchBody = $batchPayloadObject | ConvertTo-Json -Depth 10 -Compress
    $batchBody | Out-File (Join-Path $apiDir "batch-scrape-request.json") -Encoding utf8
    $batch = Invoke-RestMethod -Method Post -Uri "$baseURL/api/v1/batch/scrape" -ContentType "application/json" -Body $batchBody -TimeoutSec 30
    $batch | ConvertTo-Json -Depth 10 | Out-File (Join-Path $apiDir "batch-scrape-response.json") -Encoding utf8
    $jobID = [string]$batch.job_id
    if ([string]::IsNullOrWhiteSpace($jobID)) { throw "Gate B: no-credential batch scrape did not return job_id" }
    "job_id=$jobID" | Out-File $apiResult -Encoding utf8 -Append

    $jobUri = '{0}/api/v1/batch/{1}?include_data=true' -f $baseURL, $jobID
    "poll_uri=$jobUri" | Out-File $apiResult -Encoding utf8 -Append
    $job = $null
    $terminal = $false
    for ($i = 0; $i -lt 180; $i++) {
        Start-Sleep -Seconds 1
        $job = Invoke-RestMethod -Method Get -Uri $jobUri -TimeoutSec 15
        if ($job.status -in @("completed", "failed", "cancelled")) { $terminal = $true; break }
    }
    if ($null -ne $job) { $job | ConvertTo-Json -Depth 100 | Out-File (Join-Path $apiDir "batch-job-final.json") -Encoding utf8 }
    if (-not $terminal) { throw "Gate B: no-credential batch job did not reach terminal state" }
    "job_status=$($job.status)" | Out-File $apiResult -Encoding utf8 -Append
    "job_completed=$($job.completed)" | Out-File $apiResult -Encoding utf8 -Append
    "job_failed=$($job.failed)" | Out-File $apiResult -Encoding utf8 -Append
    if ($job.status -ne "completed" -or [int]$job.failed -ne 0) {
        throw "Gate B: no-credential batch job failed (status=$($job.status), failed=$($job.failed))"
    }

    $resultProperty = $job.results.PSObject.Properties | Where-Object { $_.Name -eq $filePath } | Select-Object -First 1
    if ($null -eq $resultProperty) { throw "Gate B: final job has no result keyed by submitted file path" }
    $fileResult = $resultProperty.Value
    $fileResult | ConvertTo-Json -Depth 100 | Out-File (Join-Path $apiDir "file-result.json") -Encoding utf8
    "file_result_status=$($fileResult.status)" | Out-File $apiResult -Encoding utf8 -Append
    "file_result_movie_id=$($fileResult.movie_id)" | Out-File $apiResult -Encoding utf8 -Append
    if ([string]$fileResult.movie_id -ne $expectedID) { throw "Gate B: movie_id=$($fileResult.movie_id), want $expectedID" }
    if ($null -eq $fileResult.movie -or [string]$fileResult.movie.id -ne $expectedID) { throw "Gate B: movie.id did not equal $expectedID" }

    $apiCombined = (Read-TextIfExists $apiStdout) + "`n" + (Read-TextIfExists $apiStderr) + "`n" + (Read-TextIfExists $apiAppLog)
    Assert-WebLookupEvidence -Combined $apiCombined -GateName "Gate B" -ResultPath $apiResult

    if ((Test-Path $credentialPath) -or (Test-Path $sessionPath)) {
        throw "Gate B: no-auth desktop run created auth.credentials.json or auth.sessions.json"
    }
    "auth_credentials_file_created=false" | Out-File $apiResult -Encoding utf8 -Append
    "auth_sessions_file_created=false" | Out-File $apiResult -Encoding utf8 -Append
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

$testedHash = (Get-FileHash $exe -Algorithm SHA256).Hash.ToLowerInvariant()
@(
    "blackbox_verification=PASS",
    "gate_a=PASS",
    "gate_b=PASS",
    "gate_b_auth_mode=desktop_local_noauth",
    "gate_c_ui=PASS",
    "auth_setup_called=false",
    "auth_login_called=false",
    "session_header_used=false",
    "auth_credentials_file_created=false",
    "auth_sessions_file_created=false",
    "expected_catalog_id=$expectedID",
    "tested_exe_sha256=$testedHash"
) | Out-File (Join-Path $evidence "blackbox-summary.txt") -Encoding utf8

Get-Content $cliResult
Get-Content $apiResult
Get-Content (Join-Path $evidence "blackbox-summary.txt")
