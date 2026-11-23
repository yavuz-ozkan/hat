# Runs install.ps1 the way "irm | iex" runs it, and hat upgrade, against fake
# releases served from a GoReleaser snapshot (internal/fakereleases), and
# checks that each installs the good release and refuses the bad ones without
# touching the hat already there. From the repository root, in Windows
# PowerShell 5.1 or PowerShell 7:
#
#   goreleaser release --snapshot --clean
#   .github/scripts/test-installers.ps1 [-RealRelease]
#
# gh is hidden from Path, so the result doesn't depend on whether this
# machine has it. -RealRelease also installs v0.1.1 from github.com with gh
# hidden, with stand-in gh programs, and with this machine's own gh.
param([switch]$RealRelease)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { throw "There's no hat build for a $($env:PROCESSOR_ARCHITECTURE) processor to test." }
}
$snapshot = Get-ChildItem -Path dist -Filter "hat_*_windows_$arch.zip" -ErrorAction SilentlyContinue | Select-Object -First 1
if (-not $snapshot) {
    Write-Host "dist\ has no windows/$arch snapshot. Build one first with goreleaser release --snapshot --clean."
    exit 2
}
$version = $snapshot.Name -replace '^hat_', '' -replace "_windows_$arch\.zip$", ''
$installer = Get-Content -Raw (Join-Path $PWD 'install.ps1')
Write-Host "PowerShell $($PSVersionTable.PSVersion) ($($PSVersionTable.PSEdition))"

function Invoke-Native([string]$Program, [string[]]$Arguments) {
    # With 'Stop', Windows PowerShell 5.1 throws on the first line a native
    # program writes to stderr.
    $ErrorActionPreference = 'Continue'
    $lines = & $Program @Arguments 2>&1 | ForEach-Object { "$_" }
    [pscustomobject]@{ Code = $LASTEXITCODE; Output = (@($lines) -join "`n") }
}

$work = Join-Path ([IO.Path]::GetTempPath()) ('hat-test-installers-' + [guid]::NewGuid())
New-Item -ItemType Directory -Path $work | Out-Null
$savedPath = $env:Path
$server = $null
try {
    foreach ($build in @(
            @('build', '-o', "$work\fakereleases.exe", './internal/fakereleases'),
            @('build', '-ldflags', '-X main.version=0.0.1', '-o', "$work\old-hat.exe", '.'))) {
        $result = Invoke-Native 'go' $build
        if ($result.Code -ne 0) { throw "go $($build -join ' ') failed: $($result.Output)" }
    }
    Set-Content -Path "$work\previous" -Value 'previous hat' -NoNewline

    $server = Start-Process -FilePath "$work\fakereleases.exe" -ArgumentList '-dist', 'dist' -WorkingDirectory $PWD -NoNewWindow -PassThru `
        -RedirectStandardOutput "$work\server.out" -RedirectStandardError "$work\server.err"
    $base = ''
    for ($try = 0; -not $base; $try++) {
        if ($try -gt 100) { throw "The fake releases server didn't start: $(Get-Content -Raw "$work\server.err")" }
        Start-Sleep -Milliseconds 100
        $base = Get-Content -Path "$work\server.out" -TotalCount 1 -ErrorAction SilentlyContinue
    }

    $noGhPath = ($env:Path -split ';' | Where-Object {
            $_ -and -not (Test-Path (Join-Path $_ 'gh.exe')) -and -not (Test-Path (Join-Path $_ 'gh.cmd')) -and -not (Test-Path (Join-Path $_ 'gh.bat'))
        }) -join ';'
    $env:Path = $noGhPath
    if (Get-Command gh -CommandType Application -ErrorAction SilentlyContinue) { throw "Couldn't hide gh from Path." }
    $env:Path = $savedPath

    $script:passed = 0
    $script:failed = 0
    $script:outcome = $null
    $script:bin = ''

    function Test-Case([string]$Name, [scriptblock]$Expectation) {
        if (& $Expectation) {
            $script:passed++
            Write-Host "PASS  $Name"
        }
        else {
            $script:failed++
            Write-Host "FAIL  $Name (exit $($script:outcome.Code)). Output:"
            $script:outcome.Output -split "`n" | ForEach-Object { Write-Host "      $_" }
        }
    }
    function Test-Said([string]$Text) { $script:outcome.Output.Contains($Text) }
    function Get-Sha([string]$File) { (Get-FileHash $File -Algorithm SHA256).Hash }
    function Test-Unchanged([string]$Reference) { (Get-Sha "$script:bin\hat.exe") -eq (Get-Sha $Reference) }
    function Get-HatVersion { (Invoke-Native "$script:bin\hat.exe" @('version')).Output }
    function Test-Installed([string]$Version) {
        $script:outcome.Code -eq 0 -and (Test-Said "Installed hat $Version to $script:bin\hat.exe.") -and (Get-HatVersion) -eq "hat $Version"
    }
    function Test-Upgraded([string]$Version) {
        $script:outcome.Code -eq 0 -and (Test-Said "Upgraded hat from 0.0.1 to $Version.") -and (Get-HatVersion) -eq "hat $Version"
    }
    function Test-Refused([string]$Text) { $script:outcome.Code -ne 0 -and (Test-Said $Text) -and (Test-Unchanged "$work\previous") }
    function Test-UpgradeRefused([string]$Text) { $script:outcome.Code -ne 0 -and (Test-Said $Text) -and (Test-Unchanged "$work\old-hat.exe") }

    function Set-Bin([string]$Name, [string]$From) {
        $script:bin = Join-Path $work $Name
        New-Item -ItemType Directory -Path $script:bin | Out-Null
        Copy-Item $From "$script:bin\hat.exe"
    }

    # Pipes the installer's text to Invoke-Expression, as irm | iex does, so
    # a failure is a thrown error rather than an exit code.
    function Invoke-Installer([string]$Bin, [string]$ReleasesUrl, [string]$HatVersion, [string]$Path) {
        $env:HAT_BIN_DIR = $Bin
        $env:HAT_RELEASES_URL = $ReleasesUrl
        $env:HAT_VERSION = $HatVersion
        $env:Path = $Path
        try {
            $lines = $installer | Invoke-Expression 6>&1 | ForEach-Object { "$_" }
            $script:outcome = [pscustomobject]@{ Code = 0; Output = (@($lines) -join "`n") }
        }
        catch {
            $script:outcome = [pscustomobject]@{ Code = 1; Output = $_.Exception.Message }
        }
        finally {
            Remove-Item Env:HAT_BIN_DIR, Env:HAT_RELEASES_URL, Env:HAT_VERSION -ErrorAction SilentlyContinue
            $env:Path = $savedPath
        }
    }
    function Invoke-Upgrade([string]$ReleasesUrl, [string]$Path) {
        $env:HAT_RELEASES_URL = $ReleasesUrl
        $env:Path = $Path
        try { $script:outcome = Invoke-Native "$script:bin\hat.exe" @('upgrade') }
        finally {
            Remove-Item Env:HAT_RELEASES_URL -ErrorAction SilentlyContinue
            $env:Path = $savedPath
        }
    }
    function Install-FakeRelease([string]$Scenario) {
        Set-Bin "install-$Scenario" "$work\previous"
        Invoke-Installer $script:bin "$base/$Scenario/releases" '' $noGhPath
    }
    function Invoke-FakeUpgrade([string]$Scenario) {
        Set-Bin "upgrade-$Scenario" "$work\old-hat.exe"
        Invoke-Upgrade "$base/$Scenario/releases" $noGhPath
    }
    $offGitHub = "Build provenance not checked: HAT_RELEASES_URL isn't a github.com address"
    $noRelease = 'no published release'

    Install-FakeRelease 'release'
    Test-Case "install.ps1 replaces the previous hat with $version" { (Test-Installed $version) -and (Test-Said $offGitHub) }
    Install-FakeRelease 'tampered'
    Test-Case 'install.ps1 refuses a tampered archive and keeps the previous hat' { Test-Refused "doesn't match its published checksum" }
    Install-FakeRelease 'no-checksum-line'
    Test-Case "install.ps1 refuses an archive checksums.txt doesn't list and keeps the previous hat" { Test-Refused "doesn't match its published checksum" }
    Install-FakeRelease 'prereleases-only'
    Test-Case "install.ps1 says there's no published release when all are pre-releases" { Test-Refused $noRelease }
    Install-FakeRelease 'no-releases'
    Test-Case "install.ps1 says there's no published release when there are none" { Test-Refused $noRelease }

    Invoke-FakeUpgrade 'release'
    Test-Case "hat upgrade replaces 0.0.1 with $version" { (Test-Upgraded $version) -and (Test-Said $offGitHub) }
    Invoke-FakeUpgrade 'tampered'
    Test-Case 'hat upgrade refuses a tampered archive and keeps the old hat' { Test-UpgradeRefused "doesn't match its published checksum" }
    Invoke-FakeUpgrade 'no-checksum-line'
    Test-Case "hat upgrade refuses an archive checksums.txt doesn't list and keeps the old hat" { Test-UpgradeRefused 'has no entry for' }
    Invoke-FakeUpgrade 'prereleases-only'
    Test-Case "hat upgrade says there's no published release when all are pre-releases" { Test-UpgradeRefused $noRelease }
    Invoke-FakeUpgrade 'no-releases'
    Test-Case "hat upgrade says there's no published release when there are none" { Test-UpgradeRefused $noRelease }

    if ($RealRelease) {
        if (-not (Get-Command gh -CommandType Application -ErrorAction SilentlyContinue)) {
            throw '-RealRelease needs gh on Path. Install it from https://cli.github.com, then run this again.'
        }
        # What gh prints for an archive GitHub has no attestation for, and what
        # gh before 2.49 prints for any attestation command.
        New-Item -ItemType Directory -Path "$work\failing-gh", "$work\old-gh" | Out-Null
        Set-Content -Path "$work\failing-gh\gh.cmd" -Value "@echo off`r`necho Error: HTTP 404: Not Found (stand-in gh from test-installers.ps1) 1>&2`r`nexit /b 1"
        Set-Content -Path "$work\old-gh\gh.cmd" -Value "@echo off`r`necho unknown command `"attestation`" for `"gh`" 1>&2`r`nexit /b 1"
        $realGhSays = if ((Invoke-Native 'gh' @('auth', 'status')).Code -eq 0) {
            'gh verified its build provenance: it was built by yavuz-ozkan/hat/.github/workflows/release.yml.'
        }
        else {
            "Build provenance not checked: gh isn't signed in. To check it next time, run gh auth login."
        }
        function Install-RealRelease([string]$Name, [string]$Path) {
            Set-Bin "real-$Name" "$work\previous"
            Invoke-Installer $script:bin '' '0.1.1' $Path
        }
        function Invoke-RealUpgrade([string]$Name, [string]$Path) {
            Set-Bin "real-upgrade-$Name" "$work\old-hat.exe"
            Invoke-Upgrade '' $Path
        }

        Install-RealRelease 'no-gh' $noGhPath
        Test-Case 'install.ps1 installs v0.1.1 from GitHub without gh, saying how to check provenance' { (Test-Installed '0.1.1') -and (Test-Said "Build provenance not checked: the GitHub CLI isn't installed.") }
        Install-RealRelease 'failing-gh' "$work\failing-gh;$noGhPath"
        Test-Case "install.ps1 refuses v0.1.1 when gh can't verify it and keeps the previous hat" { Test-Refused "gh couldn't verify it was built by" }
        Install-RealRelease 'old-gh' "$work\old-gh;$noGhPath"
        Test-Case 'install.ps1 installs v0.1.1 with a gh too old to verify, saying so' { (Test-Installed '0.1.1') -and (Test-Said 'this gh is too old to verify attestations') }
        Install-RealRelease 'gh' $savedPath
        Test-Case "install.ps1 installs v0.1.1 with this machine's gh: $realGhSays" { (Test-Installed '0.1.1') -and (Test-Said $realGhSays) }
        Invoke-RealUpgrade 'gh' $savedPath
        Test-Case "hat upgrade installs the newest GitHub release with this machine's gh" { $script:outcome.Code -eq 0 -and (Test-Said 'Upgraded hat from 0.0.1 to ') -and (Test-Said $realGhSays) }
        Invoke-RealUpgrade 'failing-gh' "$work\failing-gh;$noGhPath"
        Test-Case "hat upgrade refuses the newest GitHub release when gh can't verify it and keeps the old hat" { Test-UpgradeRefused "gh couldn't verify it was built by" }
    }

    Write-Host "$script:passed passed, $script:failed failed."
}
finally {
    $env:Path = $savedPath
    if ($server) { Stop-Process -Id $server.Id -Force -ErrorAction SilentlyContinue }
    Remove-Item -Recurse -Force $work -ErrorAction SilentlyContinue
}
if ($script:failed -ne 0) { exit 1 }
