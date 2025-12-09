#!/usr/bin/env pwsh
#Requires -Version 5.1

<#
.SYNOPSIS
    Installs the Gram CLI on Windows
.DESCRIPTION
    Downloads and installs the latest version of Gram CLI from GitHub releases
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

    $installPath = Join-Path $InstallDir "gram.exe"
    Write-Info "Installing gram to $installPath..."

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
    Write-Info "Installing Gram CLI..."

    # This script is Windows-only
    $os = "windows"

    $arch = Get-SystemArchitecture

    Write-Info "Detected architecture: $arch"

    # Get latest tag (name@version)
    $tagName = Get-LatestTag
    Write-Info "Latest version: $tagName"

    # Construct download URLs
    $filename = "gram_${os}_${arch}.zip"
    $downloadUrl = "https://github.com/speakeasy-api/gram/releases/download/${tagName}/${filename}"
    $checksumsUrl = "https://github.com/speakeasy-api/gram/releases/download/${tagName}/checksums.txt"

    # Create temporary directory
    $tmpDir = Join-Path $env:TEMP "gram-install-$(New-Guid)"
    New-Item -ItemType Directory -Path $tmpDir -Force | Out-Null

    try {
        # Download binary archive
        Write-Info "Downloading: $downloadUrl"
        $zipPath = Join-Path $tmpDir $filename
        Download-File -Url $downloadUrl -Output $zipPath

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
        $installDir = Join-Path $env:LOCALAPPDATA "Programs\gram"
        $binaryName = "gram.exe"

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
        if (Test-CommandExists "gram") {
            Write-Host ""
            & gram --version
            Write-Host ""
            Write-Host "Success! " -ForegroundColor Green -NoNewline
            Write-Host "Gram CLI has been installed."
            Write-Host "Run 'gram --help' to get started."
        }
        else {
            Write-Host ""
            Write-Host "Note: " -ForegroundColor Yellow -NoNewline
            Write-Host "Please restart your terminal for the installation to take effect."
            Write-Host "Then run 'gram --help' to get started."
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
