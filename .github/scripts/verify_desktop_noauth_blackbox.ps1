param(
    [Parameter(Mandatory = $true)]
    [string]$ExePath,

    [string]$EvidenceDir = "evidence"
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$exe = (Resolve-Path $ExePath).Path
$evidenceRoot = [System.IO.Path]::GetFullPath((Join-Path $PWD $EvidenceDir))
$gateDir = Join-Path $evidenceRoot "desktop-noauth"
New-Item -ItemType Directory -Path $gateDir -Force | Out-Null

$resultPath = Join-Path $gateDir "result.txt"
$configPath = Join-Path $gateDir "config.yaml"
$appLog = Join-Path $gateDir "app.log"
$dbPath = Join-Path $gateDir "javinizer.db"
$dumpPath = Join-Path $gateDir "r18dev_dump.db"
$stdoutPath = Join-Path $gateDir "server-stdout.txt"
$stderrPath = Join-Path $gateDir "server-stderr.txt"
$authStatusPath = Join-Path $gateDir "auth-status.json"
$batchRequestPath = Join-Path $gateDir "batch-request.json"
$batchResponsePath = Join-Path $gateDir "batch-response.json"
$jobFinalPath = Join-Path $gateDir "batch-job-final.json"
$fileResultPath = Join-Path $gateDir "file-result.json"
$credentialPath = Join-Path $gateDir "auth.credentials.json"
$sessionPath = Join-Path $gateDir "auth.sessions.json"

$titleBase = "一ヶ月間の禁欲の果てに彼女のルームメイト2人と浮気SEXだけに没頭した彼女不在の3日間"
$keepwordSuffix = "SPECIAL_4K_8K_VR_AI_字幕_中文字幕_-UC_UNCENSORED"
$titleWithKeepWords = "${titleBase}_${keepwordSuffix}"
$expectedID = "SSIS-001"
$inputDir = Join-Path $gateDir "input"
New-Item -ItemType Directory -Path $inputDir -Force | Out-Null
$filePath = Join-Path $inputDir ($titleWithKeepWords + ".mp4")
[System.IO.File]::WriteAllBytes($filePath, [byte[]](0x00, 0x01, 0x02, 0x03))

function To-YamlPath([string]$path) {
    return [System.IO.Path]::GetFullPath($path).Replace("\", "/")
}

$configSource = (Resolve-Path "configs/config.yaml.example").Path
$yaml = Get-Content $configSource -Raw
$allowed = To-YamlPath $inputDir
$log = To-YamlPath $appLog
$db = To-YamlPath $dbPath
$dump = To-YamlPath $dumpPath
$yaml = $yaml.Replace("        allowed_directories: []", "        allowed_directories:`r`n            - `"$allowed`"")
$yaml = $yaml.Replace("    file_format: <ID><IF:MULTIPART>-pt<PART></IF>", "    file_format: <ID><KEEPWORDS:SPECIAL|4K|8K|VR|AI|字幕|中文字幕|-UC|UNCENSORED;PREFIX= - ;DELIM= ><IF:MULTIPART>-pt<PART></IF>")
$yaml = $yaml.Replace("        path: data/r18dev/r18dev_dump.db", "        path: `"$dump`"")
$yaml = $yaml.Replace("    dsn: data/javinizer.db", "    dsn: `"$db`"")
$yaml = $yaml.Replace('    output: "stdout,data/logs/javinizer.log"', "    output: `"stderr,$log`"")
Set-Content -Path $configPath -Value $yaml -Encoding utf8

if (Test-Path $credentialPath) { throw "Gate C precondition failed: auth.credentials.json already exists" }
if (Test-Path $sessionPath) { throw "Gate C precondition failed: auth.sessions.json already exists" }

@(
    "gate=DESKTOP_LOCAL_NOAUTH_EXACT_EXE",
    "exact_exe=$exe",
    "tested_exe_sha256=$((Get-FileHash $exe -Algorithm SHA256).Hash.ToLowerInvariant())",
    "auth_credentials_preexisting=false",
    "auth_sessions_preexisting=false",
    "auth_setup_called=false",
    "auth_login_called=false",
    "session_header_sent=false",
    "authorization_header_sent=false",
    "cookie_session_sent=false"
) | Out-File $resultPath -Encoding utf8

$listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
$listener.Start()
$port = ([System.Net.IPEndPoint]$listener.LocalEndpoint).Port
$listener.Stop()
$baseURL = "http://127.0.0.1:$port"
"server_url=$baseURL" | Out-File $resultPath -Encoding utf8 -Append

$serverArgs = @("--config", $configPath, "--verbose", "web", "--host", "127.0.0.1", "--port", "$port")
$server = Start-Process -FilePath $exe -ArgumentList $serverArgs -PassThru -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath
"server_pid=$($server.Id)" | Out-File $resultPath -Encoding utf8 -Append

try {
    $ready = $false
    for ($i = 0; $i -lt 60; $i++) {
        Start-Sleep -Milliseconds 500
        $server.Refresh()
        if ($server.HasExited) { throw "Gate C: server exited before health check; exit=$($server.ExitCode)" }
        try {
            $health = Invoke-RestMethod -Method Get -Uri "$baseURL/health" -TimeoutSec 5
            if ($null -ne $health -and $health.status -eq "ok") {
                $health | ConvertTo-Json -Depth 10 | Out-File (Join-Path $gateDir "health.json") -Encoding utf8
                $ready = $true
                break
            }
        } catch { }
    }
    if (-not $ready) { throw "Gate C: API server did not become healthy" }

    # No WebSession, Cookie, Authorization or X-Session-ID is supplied here.
    $authStatus = Invoke-RestMethod -Method Get -Uri "$baseURL/api/v1/auth/status" -TimeoutSec 15
    $authStatus | ConvertTo-Json -Depth 20 | Out-File $authStatusPath -Encoding utf8
    "auth_status_initialized=$($authStatus.initialized)" | Out-File $resultPath -Encoding utf8 -Append
    "auth_status_authenticated=$($authStatus.authenticated)" | Out-File $resultPath -Encoding utf8 -Append
    "auth_status_username=$($authStatus.username)" | Out-File $resultPath -Encoding utf8 -Append
    $statusSession = [string]$authStatus.session_id
    "auth_status_session_id_empty=$([string]::IsNullOrWhiteSpace($statusSession))" | Out-File $resultPath -Encoding utf8 -Append

    if (-not [bool]$authStatus.initialized) { throw "Gate C: desktop-local auth status is not initialized=true" }
    if (-not [bool]$authStatus.authenticated) { throw "Gate C: desktop-local auth status is not authenticated=true" }
    if ([string]$authStatus.username -ne "local") { throw "Gate C: auth status username=$($authStatus.username), want local" }
    if (-not [string]::IsNullOrWhiteSpace($statusSession)) { throw "Gate C: auth status unexpectedly returned a session ID" }

    if (Test-Path $credentialPath) { throw "Gate C: auth status created auth.credentials.json" }
    if (Test-Path $sessionPath) { throw "Gate C: auth status created auth.sessions.json" }

    $batchPayload = @{
        files = @($filePath)
        force = $true
        selected_scrapers = @("r18dev")
        operation_mode = "preview"
    }
    $batchBody = $batchPayload | ConvertTo-Json -Depth 10 -Compress
    $batchBody | Out-File $batchRequestPath -Encoding utf8

    # This is a protected write route. Deliberately send no authentication material.
    $batch = Invoke-RestMethod -Method Post -Uri "$baseURL/api/v1/batch/scrape" -ContentType "application/json" -Body $batchBody -TimeoutSec 30
    $batch | ConvertTo-Json -Depth 20 | Out-File $batchResponsePath -Encoding utf8
    $jobID = [string]$batch.job_id
    if ([string]::IsNullOrWhiteSpace($jobID)) { throw "Gate C: unauthenticated protected batch route did not return job_id" }
    "protected_batch_without_auth=ACCEPTED" | Out-File $resultPath -Encoding utf8 -Append
    "job_id=$jobID" | Out-File $resultPath -Encoding utf8 -Append

    $jobUri = '{0}/api/v1/batch/{1}?include_data=true' -f $baseURL, $jobID
    $job = $null
    $terminal = $false
    for ($i = 0; $i -lt 180; $i++) {
        Start-Sleep -Seconds 1
        # Polling is also deliberately unauthenticated.
        $job = Invoke-RestMethod -Method Get -Uri $jobUri -TimeoutSec 15
        if ($job.status -in @("completed", "failed", "cancelled")) {
            $terminal = $true
            break
        }
    }
    if ($null -ne $job) { $job | ConvertTo-Json -Depth 100 | Out-File $jobFinalPath -Encoding utf8 }
    if (-not $terminal) { throw "Gate C: unauthenticated batch did not reach terminal state" }
    if ($job.status -ne "completed" -or [int]$job.failed -ne 0) {
        throw "Gate C: unauthenticated batch failed (status=$($job.status), failed=$($job.failed))"
    }

    $resultProperty = $job.results.PSObject.Properties | Where-Object { $_.Name -eq $filePath } | Select-Object -First 1
    if ($null -eq $resultProperty) { throw "Gate C: no result for submitted file path" }
    $fileResult = $resultProperty.Value
    $fileResult | ConvertTo-Json -Depth 100 | Out-File $fileResultPath -Encoding utf8
    if ([string]$fileResult.movie_id -ne $expectedID) {
        throw "Gate C: movie_id=$($fileResult.movie_id), want $expectedID"
    }
    if ($null -eq $fileResult.movie -or [string]$fileResult.movie.id -ne $expectedID) {
        throw "Gate C: movie.id did not equal $expectedID"
    }

    if (Test-Path $credentialPath) { throw "Gate C: protected-route use created auth.credentials.json" }
    if (Test-Path $sessionPath) { throw "Gate C: protected-route use created auth.sessions.json" }

    "job_status=$($job.status)" | Out-File $resultPath -Encoding utf8 -Append
    "job_failed=$($job.failed)" | Out-File $resultPath -Encoding utf8 -Append
    "movie_id=$($fileResult.movie_id)" | Out-File $resultPath -Encoding utf8 -Append
    "auth_credentials_created=false" | Out-File $resultPath -Encoding utf8 -Append
    "auth_sessions_created=false" | Out-File $resultPath -Encoding utf8 -Append
    "protected_batch_without_auth=PASS" | Out-File $resultPath -Encoding utf8 -Append
    "gate_c_result=PASS" | Out-File $resultPath -Encoding utf8 -Append
} finally {
    if ($null -ne $server) {
        try {
            $server.Refresh()
            if (-not $server.HasExited) {
                Stop-Process -Id $server.Id -Force
                $server.WaitForExit(10000)
            }
        } catch {
            "server_cleanup_error=$($_.Exception.Message)" | Out-File $resultPath -Encoding utf8 -Append
        }
    }
}

Get-Content $resultPath
