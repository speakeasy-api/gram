# Writes Speakeasy managed.json for the Intune-enrolled user, with an interactive fallback.
# Designed for Intune Win32 deployment as SYSTEM.
$ErrorActionPreference = 'Stop'

# Update the Intune detection rule when changing this value.
$configRevision = 'CONFIG_REVISION'

$template = @'
{
  "v": 1,
  "email": "{{mail}}",
  "org_token": "REPLACE_WITH_ORG_TOKEN",
  "org_slug": "example-corp",
  "org_name": "Example Corp",
  "auto_update": "AUTO_UPDATE_MODE"
}
'@

$dir    = Join-Path $env:ProgramData 'Speakeasy'
$file   = Join-Path $dir 'managed.json'
$daemon = Join-Path $env:ProgramFiles 'Speakeasy\speakeasyd.exe'
$log    = Join-Path $dir 'managed-install.log'

function Write-InstallLog {
    param([Parameter(Mandatory)][string]$Message)

    Add-Content -LiteralPath $log `
        -Value "$(Get-Date -Format 's')  $Message"
}

function Test-UserEmail {
    param([AllowNull()][string]$Value)

    return (
        -not [string]::IsNullOrWhiteSpace($Value) -and
        $Value -match '^[^@\s]+@[^@\s]+\.[^@\s]+$' -and
        $Value -notmatch '^(foouser|autopilot)@'
    )
}

function Get-InteractiveUserContext {
    $account = [string](Get-CimInstance Win32_ComputerSystem).UserName

    if ([string]::IsNullOrWhiteSpace($account)) {
        throw 'No interactive console user is signed in yet.'
    }

    try {
        $ntAccount = New-Object `
            -TypeName System.Security.Principal.NTAccount `
            -ArgumentList $account

        $sid = $ntAccount.Translate(
            [System.Security.Principal.SecurityIdentifier]
        ).Value
    }
    catch {
        throw "Could not resolve the SID for active user [$account]. $($_.Exception.Message)"
    }

    Write-InstallLog "Active console account: [$account]; SID: [$sid]."

    [pscustomobject]@{
        Account = $account
        Sid     = $sid
    }
}

function Get-AadNgcUserId {
    param([Parameter(Mandatory)][string]$Sid)

    $base = "Registry::HKEY_USERS\$Sid\Software\Microsoft\Windows NT\CurrentVersion\WorkplaceJoin\AADNGC"

    foreach ($key in @(Get-ChildItem -LiteralPath $base -Recurse -ErrorAction SilentlyContinue)) {
        $candidate = [string]$key.GetValue('UserId')

        if (Test-UserEmail $candidate) {
            return $candidate
        }
    }

    return $null
}

function Get-DomainUserPrincipalName {
    param([Parameter(Mandatory)][string]$Sid)

    $context = $null
    $principal = $null

    try {
        Add-Type -AssemblyName System.DirectoryServices.AccountManagement -ErrorAction Stop

        $context = New-Object `
            System.DirectoryServices.AccountManagement.PrincipalContext(
                [System.DirectoryServices.AccountManagement.ContextType]::Domain
            )

        $principal = [System.DirectoryServices.AccountManagement.UserPrincipal]::FindByIdentity(
            $context,
            [System.DirectoryServices.AccountManagement.IdentityType]::Sid,
            $Sid
        )

        if ($principal -and (Test-UserEmail $principal.UserPrincipalName)) {
            return $principal.UserPrincipalName
        }
    }
    catch {
        Write-InstallLog "AD UPN lookup did not return a usable identity: $($_.Exception.Message)"
    }
    finally {
        if ($principal) { $principal.Dispose() }
        if ($context)   { $context.Dispose() }
    }

    return $null
}

function Get-ActiveEntraUserEmail {
    $user = Get-InteractiveUserContext

    # Entra-only sign-in, normally AzureAD\user@company.com.
    if ($user.Account -match '^AzureAD\\(.+)$') {
        $candidate = $Matches[1]

        if (Test-UserEmail $candidate) {
            Write-InstallLog 'Identity source: AzureAD account name.'
            return $candidate
        }
    }

    # User-specific Entra/Workplace Join record.
    $candidate = Get-AadNgcUserId -Sid $user.Sid
    if (Test-UserEmail $candidate) {
        Write-InstallLog 'Identity source: WorkplaceJoin AADNGC UserId.'
        return $candidate
    }

    # Hybrid AD/Entra fallback.
    $candidate = Get-DomainUserPrincipalName -Sid $user.Sid
    if (Test-UserEmail $candidate) {
        Write-InstallLog 'Identity source: on-premises AD userPrincipalName.'
        return $candidate
    }

    throw "Could not determine an Entra UPN for active user [$($user.Account)] (SID $($user.Sid))."
}

function Get-EnrolledUserEmail {
    # The Intune enrollment UPN is readable as SYSTEM, even without a console session.
    foreach ($enrollment in Get-ChildItem -LiteralPath 'HKLM:\SOFTWARE\Microsoft\Enrollments' -ErrorAction SilentlyContinue) {
        $upn = (Get-ItemProperty -LiteralPath $enrollment.PSPath -ErrorAction SilentlyContinue).UPN
        if (Test-UserEmail $upn) {
            Write-InstallLog 'Identity source: Intune enrollment UPN.'
            return $upn
        }
    }

    return (Get-ActiveEntraUserEmail)
}

try {
    # Create this before identity detection so failed Intune runs leave a log.
    New-Item -ItemType Directory -Path $dir -Force | Out-Null
    Write-InstallLog "Starting. Process identity: [$env:USERDOMAIN\$env:USERNAME]."

    try {
        $config = $template | ConvertFrom-Json
    }
    catch {
        # JSON parser errors can include the configuration, including the token.
        throw 'Invalid configuration JSON. Check the template values before packaging this script.'
    }

    if ($config.org_token -like 'REPLACE_*') {
        throw 'Set org_token before packaging this script.'
    }

    if ($config.auto_update -notin 'notify', 'automatic') {
        throw 'Set auto_update to "notify" or "automatic".'
    }

    $config.email = Get-EnrolledUserEmail
    $json = $config | ConvertTo-Json -Depth 4

    [System.IO.File]::WriteAllText(
        $file,
        $json,
        [System.Text.UTF8Encoding]::new($false)
    )

    & icacls.exe $dir /inheritance:r /grant `
        '*S-1-5-18:(OI)(CI)F' `
        '*S-1-5-32-544:(OI)(CI)F' `
        '*S-1-5-32-545:(OI)(CI)RX' | Out-Null

    if ($LASTEXITCODE -ne 0) {
        throw "Failed to set directory permissions. icacls exit code: $LASTEXITCODE."
    }

    & icacls.exe $file /inheritance:r /grant `
        '*S-1-5-18:F' `
        '*S-1-5-32-544:F' `
        '*S-1-5-32-545:R' | Out-Null

    if ($LASTEXITCODE -ne 0) {
        throw "Failed to set file permissions. icacls exit code: $LASTEXITCODE."
    }

    if (Test-Path $daemon) {
        & $daemon -service restart

        if ($LASTEXITCODE -ne 0) {
            throw "speakeasyd restart failed with exit code $LASTEXITCODE."
        }
    }

    Write-InstallLog "Configuration completed. Writing managed-$configRevision.installed marker."

    Remove-Item (Join-Path $dir 'managed-*.installed') `
        -Force -ErrorAction SilentlyContinue

    New-Item -ItemType File `
        -Path (Join-Path $dir "managed-$configRevision.installed") `
        -Force | Out-Null

    exit 0
}
catch {
    try {
        if (-not (Test-Path $dir)) {
            New-Item -ItemType Directory -Path $dir -Force | Out-Null
        }

        Write-InstallLog "FAILED: $($_.Exception.Message)"
    }
    catch {
        # Preserve the original error code even if logging also fails.
    }

    exit 1
}
