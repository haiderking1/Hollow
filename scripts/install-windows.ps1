param(
    [switch]$NonInteractive
)

$ErrorActionPreference = "Stop"

function Write-Info($msg) { Write-Host "info: $msg" -ForegroundColor Cyan }
function Write-Success($msg) { Write-Host "success: $msg" -ForegroundColor Green }
function Write-Warn($msg) { Write-Host "warning: $msg" -ForegroundColor Yellow }
function Write-Err($msg) { Write-Host "error: $msg" -ForegroundColor Red }

function Get-WindowsArch {
    if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64" -or $env:PROCESSOR_ARCHITEW6432 -eq "ARM64") {
        return "arm64"
    }
    if ($env:PROCESSOR_ARCHITECTURE -eq "AMD64" -or $env:PROCESSOR_ARCHITEW6432 -eq "AMD64") {
        return "x64"
    }
    return "32-bit"
}

function Set-GitBashEnvVar {
    $candidates = @()
    $HollowHome = Join-Path $env:LOCALAPPDATA "hollow"
    $gitDir = Join-Path $HollowHome "git"

    $candidates += Join-Path $gitDir "bin\bash.exe"
    $candidates += Join-Path $gitDir "usr\bin\bash.exe"

    $gitCmd = Get-Command git -ErrorAction SilentlyContinue
    if ($gitCmd) {
        $gitExe = $gitCmd.Source
        $gitRoot = Split-Path (Split-Path $gitExe -Parent) -Parent
        $candidates += Join-Path $gitRoot "bin\bash.exe"
        $candidates += Join-Path $gitRoot "usr\bin\bash.exe"
    }

    $candidates += "${env:ProgramFiles}\Git\bin\bash.exe"
    $pf86 = [Environment]::GetEnvironmentVariable("ProgramFiles(x86)")
    if ($pf86) { $candidates += Join-Path $pf86 "Git\bin\bash.exe" }
    $candidates += "${env:LocalAppData}\Programs\Git\bin\bash.exe"

    foreach ($candidate in $candidates) {
        if ($candidate -and (Test-Path $candidate)) {
            [Environment]::SetEnvironmentVariable("HOLLOW_GIT_BASH_PATH", $candidate, "User")
            $env:HOLLOW_GIT_BASH_PATH = $candidate
            Write-Info "Set HOLLOW_GIT_BASH_PATH=$candidate"
            return $true
        }
    }

    Write-Warn "Could not locate bash.exe -- Hollow may not find Git Bash."
    Write-Info "If needed, set HOLLOW_GIT_BASH_PATH manually to your bash.exe path."
    return $false
}

function Install-Git {
    Write-Info "Checking Git..."

    if (Get-Command git -ErrorAction SilentlyContinue) {
        $version = git --version
        if (Set-GitBashEnvVar) {
            Write-Success "Git found ($version)"
            return $true
        }
        Write-Warn "git on PATH but bash.exe not found - downloading PortableGit..."
    }

    $HollowHome = Join-Path $env:LOCALAPPDATA "hollow"
    $gitDir = Join-Path $HollowHome "git"

    Write-Info "Downloading PortableGit to $gitDir\ ..."
    Write-Info '(no admin rights required; isolated from any system Git install)'

    try {
        $arch = Get-WindowsArch
        $downloadIsZip = $false

        $gitTag    = "v2.54.0.windows.1"
        $gitVer    = "2.54.0"
        $gitVerTag = "$gitVer.windows.1"

        if ($arch -eq "32-bit") {
            Write-Warn "32-bit Windows detected -- PortableGit is 64-bit only. Installing MinGit 32-bit as a last resort; bash-dependent features will be disabled/warned."
            $assetName    = "MinGit-$gitVer-32-bit.zip"
            $downloadIsZip = $true
        } elseif ($arch -eq "arm64") {
            $assetName    = "PortableGit-$gitVer-arm64.7z.exe"
            $downloadIsZip = $false
        } else {
            $assetName    = "PortableGit-$gitVer-64-bit.7z.exe"
            $downloadIsZip = $false
        }

        $downloadUrl = "https://github.com/git-for-windows/git/releases/download/$gitTag/$assetName"
        $tmpFile = Join-Path $env:TEMP $assetName

        Write-Info "Downloading $assetName (Git for Windows $gitVerTag)..."
        Invoke-WebRequest -Uri $downloadUrl -OutFile $tmpFile -UseBasicParsing

        if (Test-Path $gitDir) {
            Write-Info "Removing previous Git install at $gitDir ..."
            Remove-Item -Recurse -Force $gitDir
        }
        New-Item -ItemType Directory -Path $gitDir -Force | Out-Null

        if ($downloadIsZip) {
            Expand-Archive -Path $tmpFile -DestinationPath $gitDir -Force
        } else {
            Write-Info "Extracting PortableGit to $gitDir ..."
            $extractProc = Start-Process -FilePath $tmpFile `
                -ArgumentList "-o`"$gitDir`"", "-y" `
                -NoNewWindow -Wait -PassThru
            if ($extractProc.ExitCode -ne 0) {
                throw "PortableGit extraction failed (exit code $($extractProc.ExitCode))"
            }
        }
        Remove-Item -Force $tmpFile -ErrorAction SilentlyContinue

        $gitExe = Join-Path $gitDir "cmd\git.exe"
        if (-not (Test-Path $gitExe)) {
            throw "Git extraction did not produce git.exe at $gitExe"
        }

        # Add to session PATH
        $env:Path = "$gitDir\cmd;$env:Path"

        # Persist to User PATH
        $newPathEntries = @(
            "$gitDir\cmd",
            "$gitDir\bin",
            "$gitDir\usr\bin"
        )
        $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
        $userPathItems = if ($userPath) { $userPath -split ";" } else { @() }
        $changed = $false
        foreach ($entry in $newPathEntries) {
            if ($userPathItems -notcontains $entry) {
                $userPathItems += $entry
                $changed = $true
            }
        }
        if ($changed) {
            [Environment]::SetEnvironmentVariable("Path", ($userPathItems -join ";"), "User")
        }

        $version = & $gitExe --version
        Write-Success "Git $version installed to $gitDir (portable, user-scoped)"
        $res = Set-GitBashEnvVar
        return $res
    } catch {
        Write-Err "Could not install portable Git: $_"
        Write-Info "Fallback: install Git manually from https://git-scm.com/download/win"
        return $false
    }
}

$success = Install-Git
if (-not $success) {
    exit 1
}
