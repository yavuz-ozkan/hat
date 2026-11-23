# Runs install.ps1 the way "irm | iex" runs it, and hat upgrade, against fake
# releases served from a GoReleaser snapshot (internal/fakereleases), then
# against the newest real release on github.com with a stand-in gh (internal/fakegh)
# for every provenance outcome. Each case checks the message, that a good
# install replaced the hat already there, and that a refused one left it byte
# for byte. From the repository root, in Windows PowerShell 5.1 or
# PowerShell 7:
#
#   goreleaser release --snapshot --clean
#   .github/scripts/test-installers.ps1 [-OwnGh]
#
# The machine's own gh is hidden from Path, so the result doesn't depend on
# whether it has one or is signed in. -OwnGh also installs that release and runs
# hat upgrade with this machine's gh, which verifies when it's signed in to
# github.com.
param([switch]$OwnGh)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
Remove-Item Env:HAT_VERSION, Env:HAT_RELEASES_URL, Env:HAT_BIN_DIR, Env:HAT_TEST_GH_DEADLINE_SECONDS -ErrorAction SilentlyContinue

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
New-Item -ItemType Directory -Path $work, "$work\fakegh", "$work\broken-gh", "$work\cmd-gh" | Out-Null
$savedPath = $env:Path
$savedGhHost = $env:GH_HOST
$server = $null
$script:passed = 0
$script:failed = 0
try {
    foreach ($build in @(
            @('build', '-o', "$work\fakereleases.exe", './internal/fakereleases'),
            @('build', '-o', "$work\fakegh\gh.exe", './internal/fakegh'),
            @('build', '-ldflags', '-X main.version=0.0.1', '-o', "$work\old-hat.exe", '.'))) {
        $result = Invoke-Native 'go' $build
        if ($result.Code -ne 0) { throw "go $($build -join ' ') failed: $($result.Output)" }
    }
    Set-Content -Path "$work\previous" -Value 'previous hat' -NoNewline
    # Found on Path but can't be started: it isn't a program.
    Set-Content -Path "$work\broken-gh\gh.exe" -Value 'not a program'
    # A gh.cmd that would say yes to anything. hat only ever runs gh.exe.
    Set-Content -Path "$work\cmd-gh\gh.cmd" -Value "@echo off`r`nexit /b 0"

    $server = Start-Process -FilePath "$work\fakereleases.exe" -ArgumentList '-dist', 'dist' -WorkingDirectory $PWD -NoNewWindow -PassThru `
        -RedirectStandardOutput "$work\server.out" -RedirectStandardError "$work\server.err"
    $base = ''
    for ($try = 0; -not $base; $try++) {
        if ($try -gt 100) { throw "The fake releases server didn't start: $(Get-Content -Raw "$work\server.err")" }
        Start-Sleep -Milliseconds 100
        $base = Get-Content -Path "$work\server.out" -TotalCount 1 -ErrorAction SilentlyContinue
    }

    $noGhPath = ($env:Path -split ';' | Where-Object { $_ -and -not (Test-Path (Join-Path $_ 'gh.exe')) }) -join ';'
    $env:Path = $noGhPath
    if (Get-Command gh.exe -CommandType Application -ErrorAction SilentlyContinue) { throw "Couldn't hide gh from Path." }
    $env:Path = $savedPath
    $fakeGhPath = "$work\fakegh;$noGhPath"
    $brokenGhPath = "$work\broken-gh;$noGhPath"
    $cmdGhPath = "$work\cmd-gh;$noGhPath"

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
    function Test-Installed([string]$Version, [string]$Line) {
        $script:outcome.Code -eq 0 -and (Test-Said "Installed hat $Version to $script:bin\hat.exe.") -and (Test-Said $Line) -and (Get-HatVersion) -eq "hat $Version"
    }
    function Test-Upgraded([string]$Version, [string]$Line) {
        $script:outcome.Code -eq 0 -and (Test-Said "Upgraded hat from 0.0.1 to $Version") -and (Test-Said $Line) -and (Get-HatVersion).StartsWith("hat $Version")
    }
    function Test-Refused([string]$Text) { $script:outcome.Code -ne 0 -and (Test-Said $Text) -and (Test-Unchanged "$work\previous") }
    function Test-UpgradeRefused([string]$Text) { $script:outcome.Code -ne 0 -and (Test-Said $Text) -and (Test-Unchanged "$work\old-hat.exe") }

    function Set-Bin([string]$Name, [string]$From) {
        $script:bin = Join-Path $work $Name
        New-Item -ItemType Directory -Path $script:bin | Out-Null
        Copy-Item $From "$script:bin\hat.exe"
    }
    # Setting a variable to '' removes it, so '' means unset. An unset
    # deadline leaves gh its 2 minutes.
    function Set-RunEnvironment([string]$ReleasesUrl, [string]$Path, [string]$FakeGh, [string]$GhHost, [string]$Deadline) {
        $env:HAT_RELEASES_URL = $ReleasesUrl
        $env:Path = $Path
        $env:FAKEGH = $FakeGh
        $env:GH_HOST = $GhHost
        $env:HAT_TEST_GH_DEADLINE_SECONDS = $Deadline
    }
    function Reset-RunEnvironment {
        Remove-Item Env:HAT_BIN_DIR, Env:HAT_RELEASES_URL, Env:FAKEGH, Env:HAT_TEST_GH_DEADLINE_SECONDS -ErrorAction SilentlyContinue
        $env:GH_HOST = $savedGhHost
        $env:Path = $savedPath
    }

    # Pipes the installer's text to Invoke-Expression, as irm | iex does, so
    # a failure is a thrown error rather than an exit code.
    function Invoke-Installer([string]$ReleasesUrl, [string]$Path, [string]$FakeGh, [string]$GhHost, [string]$Deadline) {
        Set-RunEnvironment $ReleasesUrl $Path $FakeGh $GhHost $Deadline
        $env:HAT_BIN_DIR = $script:bin
        try {
            $lines = $installer | Invoke-Expression 6>&1 | ForEach-Object { "$_" }
            $script:outcome = [pscustomobject]@{ Code = 0; Output = (@($lines) -join "`n") }
        }
        catch {
            $script:outcome = [pscustomobject]@{ Code = 1; Output = $_.Exception.Message }
        }
        finally { Reset-RunEnvironment }
    }
    function Invoke-Upgrade([string]$ReleasesUrl, [string]$Path, [string]$FakeGh, [string]$GhHost, [string]$Deadline) {
        Set-RunEnvironment $ReleasesUrl $Path $FakeGh $GhHost $Deadline
        try { $script:outcome = Invoke-Native "$script:bin\hat.exe" @('upgrade') }
        finally { Reset-RunEnvironment }
    }
    function Install-FakeRelease([string]$Scenario) {
        Set-Bin "install-$Scenario" "$work\previous"
        Invoke-Installer "$base/$Scenario/releases" $noGhPath '' '' ''
    }
    function Invoke-FakeUpgrade([string]$Scenario) {
        Set-Bin "upgrade-$Scenario" "$work\old-hat.exe"
        Invoke-Upgrade "$base/$Scenario/releases" $noGhPath '' '' ''
    }
    # The newest release on github.com, which the installer finds itself.
    function Install-RealRelease([string]$Name, [string]$Path, [string]$FakeGh, [string]$GhHost, [string]$Deadline) {
        Set-Bin "real-$Name" "$work\previous"
        Invoke-Installer '' $Path $FakeGh $GhHost $Deadline
    }
    function Invoke-RealUpgrade([string]$Name, [string]$Path, [string]$FakeGh, [string]$GhHost, [string]$Deadline) {
        Set-Bin "real-upgrade-$Name" "$work\old-hat.exe"
        Invoke-Upgrade '' $Path $FakeGh $GhHost $Deadline
    }

    # The newest stable release, found the way both installers find it, so
    # each case below can check that's what it installed.
    $request = [Net.HttpWebRequest]::Create('https://github.com/yavuz-ozkan/hat/releases/latest')
    $request.Method = 'HEAD'
    $request.AllowAutoRedirect = $false
    $response = $request.GetResponse()
    $location = $response.Headers['Location']
    ([IDisposable]$response).Dispose()
    if ($location -cnotmatch '^https://github\.com/yavuz-ozkan/hat/releases/tag/v([^/]+)$') {
        throw "Couldn't find hat's newest release on github.com (sent to '$location'). Check the connection, then run this again."
    }
    $newest = $Matches[1]
    $timedOut = 'gh took longer than 3 seconds to check'

    $offGitHub = "Build provenance not checked: HAT_RELEASES_URL isn't a github.com address"
    $noRelease = 'no published release'
    $notHttps = 'but it must start with https://'
    $noGh = "Build provenance not checked: the GitHub CLI isn't installed. To check it next time, install gh from https://cli.github.com and run gh auth login."
    $verified = 'gh verified its build provenance: it was built by yavuz-ozkan/hat/.github/workflows/release.yml.'
    $signedOut = "Build provenance not checked: gh isn't signed in to github.com. To check it next time, run gh auth login --hostname github.com."
    $tooOld = 'Build provenance not checked: this gh is too old to verify attestations.'
    $cantVerify = "gh couldn't verify it was built by yavuz-ozkan/hat/.github/workflows/release.yml from tag v"
    $cantStart = "couldn't start gh to check"

    Install-FakeRelease 'release'
    Test-Case "install.ps1 replaces the previous hat with $version" { Test-Installed $version $offGitHub }
    Install-FakeRelease 'tampered'
    Test-Case 'install.ps1 refuses a tampered archive and keeps the previous hat' { Test-Refused "doesn't match its published checksum" }
    Install-FakeRelease 'no-checksum-line'
    Test-Case "install.ps1 refuses an archive checksums.txt doesn't list and keeps the previous hat" { Test-Refused "doesn't match its published checksum" }
    Install-FakeRelease 'prereleases-only'
    Test-Case "install.ps1 says there's no published release when all are pre-releases" { Test-Refused $noRelease }
    Install-FakeRelease 'no-releases'
    Test-Case "install.ps1 says there's no published release when there are none" { Test-Refused $noRelease }
    Set-Bin 'install-plain-http' "$work\previous"
    Invoke-Installer 'http://mirror.example/releases' $noGhPath '' '' ''
    Test-Case 'install.ps1 refuses a plain http:// mirror, suggesting https://' { Test-Refused $notHttps }

    Invoke-FakeUpgrade 'release'
    Test-Case "hat upgrade replaces 0.0.1 with $version" { Test-Upgraded $version $offGitHub }
    Invoke-FakeUpgrade 'tampered'
    Test-Case 'hat upgrade refuses a tampered archive and keeps the old hat' { Test-UpgradeRefused "doesn't match its published checksum" }
    Invoke-FakeUpgrade 'no-checksum-line'
    Test-Case "hat upgrade refuses an archive checksums.txt doesn't list and keeps the old hat" { Test-UpgradeRefused 'has no entry for' }
    Invoke-FakeUpgrade 'prereleases-only'
    Test-Case "hat upgrade says there's no published release when all are pre-releases" { Test-UpgradeRefused $noRelease }
    Invoke-FakeUpgrade 'no-releases'
    Test-Case "hat upgrade says there's no published release when there are none" { Test-UpgradeRefused $noRelease }
    Set-Bin 'upgrade-plain-http' "$work\old-hat.exe"
    Invoke-Upgrade 'http://mirror.example/releases' $noGhPath '' '' ''
    Test-Case 'hat upgrade refuses a plain http:// mirror, suggesting https://' { Test-UpgradeRefused $notHttps }

    Install-RealRelease 'no-gh' $noGhPath '' '' ''
    Test-Case "install.ps1 installs v$newest without gh, saying how to check provenance" { Test-Installed $newest $noGh }
    Install-RealRelease 'cmd-gh' $cmdGhPath '' '' ''
    Test-Case 'install.ps1 ignores a gh.cmd and treats it as no gh' { Test-Installed $newest $noGh }
    Install-RealRelease 'verifies' $fakeGhPath 'verifies' '' ''
    Test-Case "install.ps1 installs v$newest when gh verifies it" { Test-Installed $newest $verified }
    Install-RealRelease 'enterprise' $fakeGhPath 'verifies' 'ghe.example.com' ''
    Test-Case 'install.ps1 still asks github.com when GH_HOST is an enterprise host' { Test-Installed $newest $verified }
    Install-RealRelease 'fails' $fakeGhPath 'fails' '' ''
    Test-Case "install.ps1 refuses v$newest when gh can't verify it and keeps the previous hat" { Test-Refused $cantVerify }
    Install-RealRelease 'signed-out' $fakeGhPath 'signed-out' '' ''
    Test-Case "install.ps1 installs v$newest when gh isn't signed in to github.com, saying how to sign in" { Test-Installed $newest $signedOut }
    Install-RealRelease 'too-old' $fakeGhPath 'too-old' '' ''
    Test-Case "install.ps1 installs v$newest with a gh too old to verify, saying so" { Test-Installed $newest $tooOld }
    Install-RealRelease 'cant-start' $brokenGhPath '' '' ''
    Test-Case "install.ps1 refuses v$newest when gh can't start and keeps the previous hat" { Test-Refused $cantStart }
    Install-RealRelease 'hangs-signing-in' $fakeGhPath 'hangs-signing-in' '' '3'
    Test-Case 'install.ps1 gives up on a gh auth status that never returns and keeps the previous hat' { Test-Refused $timedOut }
    Install-RealRelease 'hangs-verifying' $fakeGhPath 'hangs-verifying' '' '3'
    Test-Case 'install.ps1 gives up on a gh attestation verify that never returns and keeps the previous hat' { Test-Refused $timedOut }

    Invoke-RealUpgrade 'no-gh' $noGhPath '' '' ''
    Test-Case "hat upgrade installs v$newest without gh, saying how to check provenance" { Test-Upgraded $newest $noGh }
    Invoke-RealUpgrade 'cmd-gh' $cmdGhPath '' '' ''
    Test-Case 'hat upgrade ignores a gh.cmd and treats it as no gh' { Test-Upgraded $newest $noGh }
    Invoke-RealUpgrade 'verifies' $fakeGhPath 'verifies' '' ''
    Test-Case "hat upgrade installs v$newest when gh verifies it" { Test-Upgraded $newest $verified }
    Invoke-RealUpgrade 'enterprise' $fakeGhPath 'verifies' 'ghe.example.com' ''
    Test-Case 'hat upgrade still asks github.com when GH_HOST is an enterprise host' { Test-Upgraded $newest $verified }
    Invoke-RealUpgrade 'fails' $fakeGhPath 'fails' '' ''
    Test-Case "hat upgrade refuses v$newest when gh can't verify it and keeps the old hat" { Test-UpgradeRefused $cantVerify }
    Invoke-RealUpgrade 'signed-out' $fakeGhPath 'signed-out' '' ''
    Test-Case "hat upgrade installs v$newest when gh isn't signed in to github.com, saying how to sign in" { Test-Upgraded $newest $signedOut }
    Invoke-RealUpgrade 'too-old' $fakeGhPath 'too-old' '' ''
    Test-Case "hat upgrade installs v$newest with a gh too old to verify, saying so" { Test-Upgraded $newest $tooOld }
    Invoke-RealUpgrade 'cant-start' $brokenGhPath '' '' ''
    Test-Case "hat upgrade refuses v$newest when gh can't start and keeps the old hat" { Test-UpgradeRefused $cantStart }
    Invoke-RealUpgrade 'hangs-signing-in' $fakeGhPath 'hangs-signing-in' '' '3'
    Test-Case 'hat upgrade gives up on a gh auth status that never returns and keeps the old hat' { Test-UpgradeRefused $timedOut }
    Invoke-RealUpgrade 'hangs-verifying' $fakeGhPath 'hangs-verifying' '' '3'
    Test-Case 'hat upgrade gives up on a gh attestation verify that never returns and keeps the old hat' { Test-UpgradeRefused $timedOut }

    if ($OwnGh) {
        if (-not (Get-Command gh.exe -CommandType Application -ErrorAction SilentlyContinue)) {
            throw '-OwnGh needs gh.exe on Path. Install it from https://cli.github.com, then run this again.'
        }
        $ownGhSays = if ((Invoke-Native 'gh.exe' @('auth', 'status', '--hostname', 'github.com')).Code -eq 0) { $verified } else { $signedOut }
        Install-RealRelease 'own-gh' $savedPath '' $savedGhHost ''
        Test-Case "install.ps1 installs v$newest with this machine's gh: $ownGhSays" { Test-Installed $newest $ownGhSays }
        Invoke-RealUpgrade 'own-gh' $savedPath '' $savedGhHost ''
        Test-Case "hat upgrade installs v$newest with this machine's gh" { Test-Upgraded $newest $ownGhSays }
    }

    Write-Host "$script:passed passed, $script:failed failed."
}
finally {
    $env:Path = $savedPath
    $env:GH_HOST = $savedGhHost
    if ($server) { Stop-Process -Id $server.Id -Force -ErrorAction SilentlyContinue }
    Remove-Item -Recurse -Force $work -ErrorAction SilentlyContinue
}
# GitHub Actions ends a PowerShell step with "exit $LASTEXITCODE", which is
# whatever the last program here returned, often a hat upgrade meant to fail.
if ($script:failed -ne 0) { exit 1 }
exit 0
