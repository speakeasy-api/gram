# speakeasy-hooks installer for Windows.
#
# Designed for quick installs over the network and CI/CD:
#   irm https://raw.githubusercontent.com/speakeasy-api/gram/main/hooks/install.ps1 | iex
#
# Environment overrides:
#   $env:VERSION      release version to install (default: latest hooks@ release)
#   $env:INSTALL_DIR  target directory (default: %LOCALAPPDATA%\Programs\speakeasy-hooks)

$ErrorActionPreference = "Stop"

$Repo = "speakeasy-api/gram"
$Binary = "speakeasy-hooks"
$InstallDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "Programs\$Binary" }

$Arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    "AMD64" { "amd64" }
    "ARM64" { "arm64" }
    default { throw "unsupported architecture: $($env:PROCESSOR_ARCHITECTURE)" }
}

$Version = $env:VERSION
if (-not $Version) {
    # Releases in this repository are shared across components; hooks releases
    # are tagged hooks@<version> and can sit pages deep under the more
    # frequent server/dashboard releases.
    for ($Page = 1; $Page -le 10 -and -not $Version; $Page++) {
        $Releases = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases?per_page=100&page=$Page"
        if (-not $Releases) { break }
        $HooksRelease = $Releases | Where-Object { $_.tag_name -like "hooks@*" } | Select-Object -First 1
        if ($HooksRelease) { $Version = $HooksRelease.tag_name -replace "^hooks@", "" }
    }
    if (-not $Version) { throw "could not resolve the latest $Binary release" }
}

$Archive = "${Binary}_windows_${Arch}.zip"
$Url = "https://github.com/$Repo/releases/download/hooks%40$Version/$Archive"

$TmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Path $TmpDir | Out-Null
try {
    Write-Host "Downloading $Binary $Version (windows/$Arch)..."
    $ZipPath = Join-Path $TmpDir $Archive
    Invoke-WebRequest -Uri $Url -OutFile $ZipPath
    Expand-Archive -Path $ZipPath -DestinationPath $TmpDir -Force

    $BinPath = Get-ChildItem -Path $TmpDir -Filter "$Binary.exe" -Recurse | Select-Object -First 1
    if (-not $BinPath) { throw "archive did not contain $Binary.exe" }

    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    Move-Item -Path $BinPath.FullName -Destination (Join-Path $InstallDir "$Binary.exe") -Force

    # Add the install directory to the user PATH when missing.
    $UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($UserPath -notlike "*$InstallDir*") {
        [Environment]::SetEnvironmentVariable("Path", "$UserPath;$InstallDir", "User")
        Write-Host "Added $InstallDir to your user PATH (restart your terminal to pick it up)."
    }

    $Installed = Join-Path $InstallDir "$Binary.exe"
    Write-Host "Installed $Installed ($(& $Installed --version))"
}
finally {
    Remove-Item -Recurse -Force $TmpDir -ErrorAction SilentlyContinue
}
