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
#>

param()

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

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

function Test-CommandExists {
    param([string]$Command)
    $null -ne (Get-Command $Command -ErrorAction SilentlyContinue)
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

    # Construct download URLs. Releases made before the CLI was renamed only
    # publish gram archives, so fall back to those.
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
        try {
            Invoke-WebRequest -Uri "${releaseUrl}/${filename}" -OutFile $zipPath -UseBasicParsing
        }
        catch {
            $archiveName = "gram"
            $filename = "${archiveName}_${os}_${arch}.zip"
            $zipPath = Join-Path $tmpDir $filename
            Write-Info "Downloading: ${releaseUrl}/${filename}"
            Download-File -Url "${releaseUrl}/${filename}" -Output $zipPath
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

        # Determine install location and binary name (Windows-only)
        $installDir = Join-Path $env:LOCALAPPDATA "Programs\speakeasy"
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

        # Verify installation
        if (Test-CommandExists "speakeasy") {
            Write-Host ""
            & speakeasy --version
            Write-Host ""
            Write-Host "Success! " -ForegroundColor Green -NoNewline
            Write-Host "The speakeasy CLI has been installed."
            Write-Host "Run 'speakeasy --help' to get started."
        }
        else {
            Write-Host ""
            Write-Host "Note: " -ForegroundColor Yellow -NoNewline
            Write-Host "Please restart your terminal for the installation to take effect."
            Write-Host "Then run 'speakeasy --help' to get started."
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
