# Installs hat into ~\.local\bin. Run it in PowerShell with:
#   irm https://raw.githubusercontent.com/yavuz-ozkan/hat/main/install.ps1 | iex
#
# iex runs this inside your own PowerShell session. So everything sits in one
# script block, which keeps settings like $ErrorActionPreference from leaking
# into your session, and a failure throws instead of calling exit, which
# would close your window.
& {
    $ErrorActionPreference = 'Stop'
    # Windows PowerShell 5.1 redraws its progress bar for every downloaded
    # chunk, which makes a download many times slower.
    $ProgressPreference = 'SilentlyContinue'

    $repo = 'yavuz-ozkan/hat'
    $releases = "https://github.com/$repo/releases"
    $binDir = if ($env:HAT_BIN_DIR) { $env:HAT_BIN_DIR } else { Join-Path $HOME '.local\bin' }

    function Stop-Install([string]$Message) {
        throw "hat install: $Message"
    }

    $arch = switch ($env:PROCESSOR_ARCHITECTURE) {
        'AMD64' { 'amd64' }
        'ARM64' { 'arm64' }
        default { Stop-Install "there's no hat build for a $($env:PROCESSOR_ARCHITECTURE) processor." }
    }

    if ($env:HAT_VERSION) {
        $tag = 'v' + $env:HAT_VERSION.Trim().TrimStart('v')
    }
    else {
        # /releases/latest redirects to /releases/tag/<tag>. Reading where it
        # points, without following it, needs no GitHub API token. HEAD,
        # because Windows PowerShell 5.1 reports GitHub's 404 page for a GET
        # as a dropped connection.
        $request = [Net.HttpWebRequest]::Create("$releases/latest")
        $request.Method = 'HEAD'
        $request.AllowAutoRedirect = $false
        try {
            $response = $request.GetResponse()
        }
        catch [Net.WebException] {
            # Windows PowerShell 5.1 wraps the WebException; PowerShell 7 doesn't.
            $failure = $_.Exception
            if ($failure.InnerException -is [Net.WebException]) { $failure = $failure.InnerException }
            if (-not $failure.Response) {
                Stop-Install "couldn't reach GitHub to look up the newest release ($($failure.Message)). Check your connection, then run this again."
            }
            $response = $failure.Response
        }
        $status = [int]$response.StatusCode
        $location = $response.Headers['Location']
        ([IDisposable]$response).Dispose()
        if ($status -eq 404) { Stop-Install "GitHub lists no published release at $releases yet." }
        if ($status -lt 300 -or $status -ge 400) { Stop-Install "GitHub answered HTTP $status when looking up the newest release. Run this again later." }
        if ($location -notmatch '/releases/tag/([^/]+)$') {
            Stop-Install "GitHub sent the newest-release lookup to '$location', which isn't a release page, so the repository may have moved. Find where hat lives now and install from there."
        }
        $tag = $Matches[1]
    }
    $version = $tag.TrimStart('v')
    $asset = "hat_${version}_windows_$arch.zip"

    $tmp = Join-Path ([IO.Path]::GetTempPath()) ('hat-install-' + [guid]::NewGuid())
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        foreach ($name in @('checksums.txt', $asset)) {
            try {
                Invoke-WebRequest -UseBasicParsing -Uri "$releases/download/$tag/$name" -OutFile (Join-Path $tmp $name)
            }
            catch {
                Stop-Install "couldn't download $name from release $tag ($($_.Exception.Message)). Check that $releases/tag/$tag exists and has a build for windows/$arch."
            }
        }

        $line = Get-Content (Join-Path $tmp 'checksums.txt') | Where-Object { ($_ -split '\s+')[1] -eq $asset } | Select-Object -First 1
        $expected = if ($line) { ($line -split '\s+')[0].ToLower() } else { '' }
        $actual = (Get-FileHash (Join-Path $tmp $asset) -Algorithm SHA256).Hash.ToLower()
        if ($expected -ne $actual) {
            Stop-Install "$asset doesn't match its published checksum, so nothing was installed. Run this again; if it keeps failing, the release is damaged."
        }

        Expand-Archive -Path (Join-Path $tmp $asset) -DestinationPath (Join-Path $tmp 'unpacked')
        New-Item -ItemType Directory -Force -Path $binDir | Out-Null
        $target = Join-Path $binDir 'hat.exe'
        $staged = "$target.new-" + [guid]::NewGuid()
        Copy-Item -Force (Join-Path $tmp 'unpacked\hat.exe') $staged
        # A running program can't be overwritten on Windows, but it can be renamed.
        # Each copy moved aside gets its own name: one from an earlier install may
        # still be running a Claude session and can't be replaced yet.
        $movedAside = ''
        if (Test-Path $target) {
            $movedAside = "$target.moved-aside-by-hat-" + [DateTime]::UtcNow.Ticks
            try {
                Move-Item -Force $target $movedAside
            }
            catch {
                Remove-Item -Force $staged -ErrorAction SilentlyContinue
                Stop-Install "couldn't move the installed hat aside ($($_.Exception.Message)). Your current hat is unchanged."
            }
        }
        try {
            Move-Item -Force $staged $target
        }
        catch {
            $installError = $_.Exception.Message
            Remove-Item -Force $staged -ErrorAction SilentlyContinue
            if ($movedAside) {
                try { Move-Item -Force $movedAside $target }
                catch { Stop-Install "couldn't put the new hat in place ($installError), and couldn't put the old one back. Your previous hat is at ${movedAside}: rename it to hat.exe to keep using it." }
            }
            Stop-Install "couldn't put the new hat in place ($installError). Your previous hat is still installed."
        }
        Write-Host "Installed hat $version to $target."
        Get-ChildItem -Path $binDir -Filter 'hat.exe.moved-aside-by-hat-*' |
            Where-Object { $_.Name -match '^hat\.exe\.moved-aside-by-hat-\d+$' } |
            Remove-Item -Force -ErrorAction SilentlyContinue

        $searched = [Environment]::GetEnvironmentVariable('Path', 'User') + ';' + [Environment]::GetEnvironmentVariable('Path', 'Machine')
        if (($searched -split ';') -notcontains $binDir) {
            $quoted = $binDir -replace "'", "''"
            Write-Host "$binDir isn't on your Path yet. Run this once, then open a new terminal:"
            Write-Host "  [Environment]::SetEnvironmentVariable('Path', [Environment]::GetEnvironmentVariable('Path', 'User') + ';$quoted', 'User')"
        }
    }
    finally {
        Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    }
}
