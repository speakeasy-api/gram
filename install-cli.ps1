#!/usr/bin/env pwsh
#Requires -Version 5.1

<#
.SYNOPSIS
    Installs the speakeasy CLI on Windows
.DESCRIPTION
    Downloads and installs the latest version of the speakeasy CLI from GitHub releases
.EXAMPLE
    .\install-cli.ps1
.EXAMPLE
    iwr -useb https://raw.githubusercontent.com/speakeasy-api/gram/main/install-cli.ps1 | iex
.NOTES
    Set the INSTALL_DIR environment variable to install somewhere other than
    %LOCALAPPDATA%\Programs\speakeasy.
#>

param()

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

# Printed by `speakeasy --control-plane-cli`. The Speakeasy SDK generator CLI
# also installs a `speakeasy` binary and rejects that flag.
$ControlPlaneMarker = "speakeasy-ai-control-plane-cli"

# Functions
function Write-Info {
    param([string]$Message)
    Write-Host "==> " -ForegroundColor Blue -NoNewline
    Write-Host $Message
}

function Write-ErrorMsg {
    param([string]$Message)
    Write-Host "Error: " -ForegroundColor Red -NoNewline
    Write-Host $Message -ForegroundColor Red
    exit 1
}

function Write-Warning {
    param([string]$Message)
    Write-Host "Warning: " -ForegroundColor Yellow -NoNewline
    Write-Host $Message -ForegroundColor Yellow
}

function Get-SystemArchitecture {
    $arch = [System.Environment]::GetEnvironmentVariable("PROCESSOR_ARCHITECTURE")
    switch ($arch) {
        "AMD64" { return "amd64" }
        "ARM64" { return "arm64" }
        "x86" { return "386" }
        default {
            Write-ErrorMsg "Unsupported architecture: $arch"
        }
    }
}

function Get-LatestTag {
    Write-Info "Fetching latest version..."

    $packageUrl = "https://raw.githubusercontent.com/speakeasy-api/gram/refs/heads/main/cli/package.json"

    try {
        $response = Invoke-RestMethod -Uri $packageUrl -UseBasicParsing
    }
    catch {
        Write-ErrorMsg "Failed to fetch package.json from GitHub: $_"
    }

    $name = $response.name
    $version = $response.version

    if (-not $name -or -not $version) {
        Write-ErrorMsg "Failed to extract name or version from package.json"
    }

    return "${name}@${version}"
}

function Download-File {
    param(
        [string]$Url,
        [string]$Output
    )

    try {
        Invoke-WebRequest -Uri $Url -OutFile $Output -UseBasicParsing
    }
    catch {
        Write-ErrorMsg "Failed to download from $Url : $_"
    }
}

# Runs a native command and returns its stdout lines, or $null when it fails.
function Invoke-Quietly {
    param(
        [string]$Path,
        [string[]]$Arguments
    )

    # Windows PowerShell turns native stderr into terminating errors under
    # 'Stop', so relax it for this probe only.
    $ErrorActionPreference = 'Continue'
    try {
        $output = & $Path @Arguments 2>$null
        if ($LASTEXITCODE -ne 0) {
            return $null
        }
        return @($output)
    }
    catch {
        return $null
    }
}

# Reports whether the binary at $Path is the Speakeasy AI Control Plane CLI.
function Test-ControlPlaneCli {
    param([string]$Path)
    $output = Invoke-Quietly -Path $Path -Arguments @("--control-plane-cli")
    return ($null -ne $output) -and ($output -ccontains $ControlPlaneMarker)
}

# Reports whether the binary at $Path is a build of this CLI from before the
# rename. Those builds have no marker flag and print "gram version ...".
function Test-LegacyCli {
    param([string]$Path)
    $output = Invoke-Quietly -Path $Path -Arguments @("--version")
    return ($null -ne $output) -and [bool]($output | Where-Object { $_ -clike "gram version *" })
}

# Downloads a release asset. Returns $true on success and $false when the
# server answers 404. Any other failure stops the script with the real error.
function Get-ReleaseAsset {
    param(
        [string]$Url,
        [string]$Output
    )

    try {
        Invoke-WebRequest -Uri $Url -OutFile $Output -UseBasicParsing
        return $true
    }
    catch {
        $response = $_.Exception.Response
        if ($null -ne $response -and [int]$response.StatusCode -eq 404) {
            return $false
        }
        Write-ErrorMsg "Failed to download from $Url : $_"
    }
}

function Test-Checksum {
    param(
        [string]$File,
        [string]$ChecksumsFile,
        [string]$Filename
    )

    Write-Info "Verifying checksum..."

    # Read checksums file and find the expected checksum
    $checksumContent = Get-Content $ChecksumsFile
    $expectedChecksum = ($checksumContent | Where-Object { $_ -match [regex]::Escape($Filename) } | ForEach-Object {
        ($_ -split '\s+')[0]
    })

    if (-not $expectedChecksum) {
        Write-ErrorMsg "Checksum not found for $Filename"
    }

    # Calculate actual checksum
    $actualChecksum = (Get-FileHash -Path $File -Algorithm SHA256).Hash.ToLower()

    if ($expectedChecksum -ne $actualChecksum) {
        Write-ErrorMsg "Checksum verification failed!`nExpected: $expectedChecksum`nActual: $actualChecksum"
    }

    Write-Info "Checksum verified successfully"
}

function Install-Binary {
    param(
        [string]$BinaryPath,
        [string]$InstallDir
    )

    $installPath = Join-Path $InstallDir "speakeasy.exe"
    Write-Info "Installing speakeasy to $installPath..."

    # Create install directory if it doesn't exist
    if (-not (Test-Path $InstallDir)) {
        try {
            New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
        }
        catch {
            Write-ErrorMsg "Failed to create directory $InstallDir : $_"
        }
    }

    # Move binary
    try {
        Move-Item -Path $BinaryPath -Destination $installPath -Force
    }
    catch {
        Write-ErrorMsg "Failed to install binary: $_"
    }

    Write-Info "Installation complete!"
}

function Main {
    Write-Info "Installing the speakeasy CLI..."

    # This script is Windows-only
    $os = "windows"

    $arch = Get-SystemArchitecture

    Write-Info "Detected architecture: $arch"

    # Get latest tag (name@version)
    $tagName = Get-LatestTag
    Write-Info "Latest version: $tagName"

    # Determine install location. INSTALL_DIR overrides the default.
    $installDir = $env:INSTALL_DIR
    if (-not $installDir) {
        $installDir = Join-Path $env:LOCALAPPDATA "Programs\speakeasy"
    }
    $installPath = Join-Path $installDir "speakeasy.exe"

    # Never overwrite the Speakeasy SDK generator CLI, which installs a binary
    # with the same name.
    if ((Test-Path $installPath) -and -not (Test-ControlPlaneCli $installPath) -and -not (Test-LegacyCli $installPath)) {
        Write-ErrorMsg "$installPath already exists and is not the Speakeasy AI Control Plane CLI. It looks like the Speakeasy SDK CLI, which also installs a 'speakeasy' binary. Install to another directory by setting INSTALL_DIR, for example: `$env:INSTALL_DIR = `"`$env:USERPROFILE\bin`"; iwr -useb https://raw.githubusercontent.com/speakeasy-api/gram/main/install-cli.ps1 | iex"
    }

    # Construct download URLs. Releases made before the CLI was renamed only
    # publish gram archives, so fall back to those when the speakeasy archive
    # does not exist.
    $releaseUrl = "https://github.com/speakeasy-api/gram/releases/download/${tagName}"
    $archiveName = "speakeasy"
    $filename = "${archiveName}_${os}_${arch}.zip"
    $checksumsUrl = "${releaseUrl}/checksums.txt"

    # Create temporary directory
    $tmpDir = Join-Path $env:TEMP "speakeasy-install-$(New-Guid)"
    New-Item -ItemType Directory -Path $tmpDir -Force | Out-Null

    try {
        # Download binary archive
        Write-Info "Downloading: ${releaseUrl}/${filename}"
        $zipPath = Join-Path $tmpDir $filename
        if (-not (Get-ReleaseAsset -Url "${releaseUrl}/${filename}" -Output $zipPath)) {
            $archiveName = "gram"
            $filename = "${archiveName}_${os}_${arch}.zip"
            $zipPath = Join-Path $tmpDir $filename
            Write-Info "No speakeasy archive in ${tagName}. Downloading: ${releaseUrl}/${filename}"
            if (-not (Get-ReleaseAsset -Url "${releaseUrl}/${filename}" -Output $zipPath)) {
                Write-ErrorMsg "No CLI archive for ${os}/${arch} in ${tagName}"
            }
        }

        # Download checksums
        Write-Info "Downloading checksums..."
        $checksumsPath = Join-Path $tmpDir "checksums.txt"
        Download-File -Url $checksumsUrl -Output $checksumsPath

        # Verify checksum
        Test-Checksum -File $zipPath -ChecksumsFile $checksumsPath -Filename $filename

        # Extract binary
        Write-Info "Extracting binary..."
        try {
            Expand-Archive -Path $zipPath -DestinationPath $tmpDir -Force
        }
        catch {
            Write-ErrorMsg "Failed to extract archive: $_"
        }

        $binaryName = "${archiveName}.exe"

        $binaryPath = Join-Path $tmpDir $binaryName

        # Install binary
        Install-Binary -BinaryPath $binaryPath -InstallDir $installDir

        # Add to PATH if not already present
        $userPath = [System.Environment]::GetEnvironmentVariable("Path", "User")
        if ($userPath -notlike "*$installDir*") {
            Write-Info "Adding $installDir to user PATH..."
            [System.Environment]::SetEnvironmentVariable(
                "Path",
                "$userPath;$installDir",
                "User"
            )
            Write-Warning "Please restart your terminal for PATH changes to take effect"
        }

        # Reload PATH for verification
        $env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User")

        # Verify the binary just installed, not whichever speakeasy is first on
        # PATH. Builds from before the rename have no marker flag.
        Write-Host ""
        & $installPath --version
        if (($archiveName -eq "speakeasy") -and -not (Test-ControlPlaneCli $installPath)) {
            Write-ErrorMsg "$installPath did not identify as the Speakeasy AI Control Plane CLI"
        }
        Write-Host ""
        Write-Host "Success! " -ForegroundColor Green -NoNewline
        Write-Host "The speakeasy CLI has been installed to $installPath."

        $onPath = (Get-Command speakeasy -ErrorAction SilentlyContinue | Select-Object -First 1).Source
        if (-not $onPath) {
            Write-Host "Note: " -ForegroundColor Yellow -NoNewline
            Write-Host "Please restart your terminal for the installation to take effect."
            Write-Host "Then run 'speakeasy --help' to get started."
        }
        elseif ($onPath -ne $installPath) {
            Write-Warning "'speakeasy' on your PATH resolves to $onPath, not $installPath. That may be the Speakeasy SDK CLI. Put $installDir earlier on your PATH or run $installPath directly."
        }
        else {
            Write-Host "Run 'speakeasy --help' to get started."
        }
    }
    finally {
        # Cleanup
        if (Test-Path $tmpDir) {
            Remove-Item -Path $tmpDir -Recurse -Force -ErrorAction SilentlyContinue
        }
    }
}

# Run main function
Main
