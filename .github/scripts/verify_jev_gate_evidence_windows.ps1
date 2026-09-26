param(
    [string]$EvidenceDir = "evidence"
)

$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($env:TYPESAFE_API_KEY)) {
    throw "TYPESAFE_API_KEY is required for Jev evidence verification"
}

$root = [System.IO.Path]::GetFullPath((Join-Path $PWD $EvidenceDir))
$thresholdRequired = 0.80

function Read-Text([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path)) {
        throw "required evidence file is missing: $Path"
    }
    return Get-Content -LiteralPath $Path -Raw
}

function Read-CombinedLog([string]$Dir) {
    $parts = @()
    foreach ($name in @("stdout.txt", "stderr.txt", "server-stdout.txt", "server-stderr.txt", "app.log")) {
        $path = Join-Path $Dir $name
        if (Test-Path -LiteralPath $path) {
            $parts += (Get-Content -LiteralPath $path -Raw)
        }
    }
    return ($parts -join "`n")
}

function Get-Value([string]$Text, [string]$Key) {
    $m = [regex]::Match($Text, "(?m)^" + [regex]::Escape($Key) + "=(?<value>.*)$")
    if (-not $m.Success) { return "" }
    return $m.Groups["value"].Value.Trim()
}

function Assert-JevGate {
    param(
        [Parameter(Mandatory = $true)][string]$Name,
        [Parameter(Mandatory = $true)][string]$LogText,
        [Parameter(Mandatory = $true)][string]$ExpectedID
    )

    $webMatches = [regex]::Matches(
        $LogText,
        'web search provider=(?<provider>[^ ]+) query="(?<query>.*?)" results=(?<results>[1-9][0-9]*)'
    )
    if ($webMatches.Count -eq 0) {
        throw "$Name: no organic Web-search result evidence"
    }

    $jevMatches = [regex]::Matches(
        $LogText,
        'Jev catalog gate accepted candidate=(?<candidate>[^ ]+) probability=(?<prob>[0-9.]+) threshold=(?<threshold>[0-9.]+) model=(?<model>[^\s]+)'
    )
    if ($jevMatches.Count -eq 0) {
        throw "$Name: no Jev acceptance evidence"
    }

    $jev = $jevMatches[$jevMatches.Count - 1]
    $candidate = $jev.Groups["candidate"].Value
    $probability = [double]::Parse($jev.Groups["prob"].Value, [System.Globalization.CultureInfo]::InvariantCulture)
    $threshold = [double]::Parse($jev.Groups["threshold"].Value, [System.Globalization.CultureInfo]::InvariantCulture)
    $model = $jev.Groups["model"].Value

    if ($candidate -ne $ExpectedID) {
        throw "$Name: Jev accepted $candidate, expected $ExpectedID"
    }
    if ($probability -lt $thresholdRequired) {
        throw "$Name: Jev probability $probability is below required $thresholdRequired"
    }
    if ([math]::Abs($threshold - $thresholdRequired) -gt 0.000001) {
        throw "$Name: runtime Jev threshold $threshold does not equal required $thresholdRequired"
    }

    $web = $webMatches[$webMatches.Count - 1]
    return [pscustomobject]@{
        gate = $Name
        expected_catalog_id = $ExpectedID
        web_provider = $web.Groups["provider"].Value
        web_results = [int]$web.Groups["results"].Value
        web_query = $web.Groups["query"].Value
        jev_candidate = $candidate
        jev_probability = $probability
        jev_threshold = $threshold
        jev_model = $model
        result = "PASS"
    }
}

$abSummary = Read-Text (Join-Path $root "blackbox-summary.txt")
if (Get-Value $abSummary "blackbox_verification" -ne "PASS") {
    throw "Gate A/B base black-box verification is not PASS"
}
if (Get-Value $abSummary "gate_a" -ne "PASS" -or Get-Value $abSummary "gate_b" -ne "PASS") {
    throw "Gate A/B base gates are not PASS"
}
$expectedAB = Get-Value $abSummary "expected_catalog_id"
if ([string]::IsNullOrWhiteSpace($expectedAB)) {
    throw "Gate A/B expected catalog ID is missing"
}

$gateCResult = Read-Text (Join-Path $root "desktop-noauth/result.txt")
if (Get-Value $gateCResult "gate_c_result" -ne "PASS") {
    throw "Gate C base verification is not PASS"
}
$expectedC = Get-Value $gateCResult "movie_id"
if ([string]::IsNullOrWhiteSpace($expectedC)) {
    throw "Gate C movie_id is missing"
}

$records = @(
    (Assert-JevGate -Name "A" -LogText (Read-CombinedLog (Join-Path $root "blackbox-cli")) -ExpectedID $expectedAB),
    (Assert-JevGate -Name "B" -LogText (Read-CombinedLog (Join-Path $root "blackbox-api")) -ExpectedID $expectedAB),
    (Assert-JevGate -Name "C" -LogText (Read-CombinedLog (Join-Path $root "desktop-noauth")) -ExpectedID $expectedC)
)

$hashAB = Get-Value $abSummary "tested_exe_sha256"
$hashC = Get-Value $gateCResult "tested_exe_sha256"
if ([string]::IsNullOrWhiteSpace($hashAB) -or [string]::IsNullOrWhiteSpace($hashC) -or $hashAB -ne $hashC) {
    throw "Gate A/B/C EXE SHA identity failed"
}

$jsonPath = Join-Path $root "jev-gate-evidence.json"
$summaryPath = Join-Path $root "jev-gate-summary.txt"
$records | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath $jsonPath -Encoding utf8

@(
    "jev_gate_verification=PASS",
    "threshold=0.80",
    "gate_a=PASS",
    "gate_b=PASS",
    "gate_c=PASS",
    "gate_a_probability=$($records[0].jev_probability)",
    "gate_b_probability=$($records[1].jev_probability)",
    "gate_c_probability=$($records[2].jev_probability)",
    "gate_a_provider=$($records[0].web_provider)",
    "gate_b_provider=$($records[1].web_provider)",
    "gate_c_provider=$($records[2].web_provider)",
    "tested_exe_sha256=$hashAB"
) | Set-Content -LiteralPath $summaryPath -Encoding utf8

Get-Content -LiteralPath $summaryPath
