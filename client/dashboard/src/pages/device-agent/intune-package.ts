import { buildIntuneWin } from "@/lib/intunewin";
import { zipSync } from "fflate";

// The Intune bundle replaces the manual IntuneWinAppUtil step: three Win32
// apps (identity config, agent msi, tray startup shortcut) packaged and
// ready to upload, with the Program and Detection settings in a README.

const MSI_UPGRADE_CODE = "{368EC0A6-8862-4906-AF34-8C3ADB92C6E0}";

const CONFIG_SCRIPT = "Deploy-SpeakeasyManagedJson.ps1";
const STARTUP_SCRIPT = "Deploy-SpeakeasyStartup.ps1";
const DETECT_SCRIPT = "Detect-SpeakeasyAgent.ps1";
const AGENT_INSTALL_SCRIPT = "Install-SpeakeasyAgent.cmd";

const POWERSHELL =
  "%windir%\\sysnative\\WindowsPowerShell\\v1.0\\powershell.exe -NoProfile -ExecutionPolicy Bypass -File";

export type IntuneConfig = {
  orgToken: string;
  orgSlug: string;
  orgName: string;
  autoUpdate: "automatic" | "notify";
  revision: string;
};

// PowerShell treats the typographic single quotes as quote characters too,
// so all of them are doubled inside a single-quoted literal.
export function psQuote(value: string): string {
  return `'${value.replace(/['‘’‚‛]/g, (q) => q + q)}'`;
}

export function configRevision(now: Date): string {
  return now
    .toISOString()
    .replace(/[-:TZ]/g, "")
    .slice(0, 14);
}

export function configScript(c: IntuneConfig): string {
  const template = JSON.stringify(
    {
      v: 1,
      email: "{{mail}}",
      org_token: c.orgToken,
      org_slug: c.orgSlug,
      org_name: c.orgName,
      auto_update: c.autoUpdate,
    },
    null,
    2,
  );
  // Template from the manual Intune docs (marketing-site c3dd892fdb74).
  return `# Writes Speakeasy managed.json for the Intune-enrolled user, with an interactive fallback.
# Designed for Intune Win32 deployment as SYSTEM.
$ErrorActionPreference = 'Stop'

# Update the Intune detection rule when changing this value.
$configRevision = ${psQuote(c.revision)}

$template = @'
${template}
'@

$dir    = Join-Path $env:ProgramData 'Speakeasy'
$file   = Join-Path $dir 'managed.json'
$daemon = Join-Path $env:ProgramFiles 'Speakeasy\\speakeasyd.exe'
$log    = Join-Path $dir 'managed-install.log'

function Write-InstallLog {
    param([Parameter(Mandatory)][string]$Message)

    Add-Content -LiteralPath $log \`
        -Value "$(Get-Date -Format 's')  $Message"
}

function Test-UserEmail {
    param([AllowNull()][string]$Value)

    return (
        -not [string]::IsNullOrWhiteSpace($Value) -and
        $Value -match '^[^@\\s]+@[^@\\s]+\\.[^@\\s]+$' -and
        $Value -notmatch '^(foouser|autopilot)@'
    )
}

function Get-InteractiveUserContext {
    $account = [string](Get-CimInstance Win32_ComputerSystem).UserName

    if ([string]::IsNullOrWhiteSpace($account)) {
        throw 'No interactive console user is signed in yet.'
    }

    try {
        $ntAccount = New-Object \`
            -TypeName System.Security.Principal.NTAccount \`
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

    $base = "Registry::HKEY_USERS\\$Sid\\Software\\Microsoft\\Windows NT\\CurrentVersion\\WorkplaceJoin\\AADNGC"

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

        $context = New-Object \`
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

    # Entra-only sign-in, normally AzureAD\\user@company.com.
    if ($user.Account -match '^AzureAD\\\\(.+)$') {
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
    foreach ($enrollment in Get-ChildItem -LiteralPath 'HKLM:\\SOFTWARE\\Microsoft\\Enrollments' -ErrorAction SilentlyContinue) {
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
    Write-InstallLog "Starting. Process identity: [$env:USERDOMAIN\\$env:USERNAME]."

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

    & icacls.exe $dir /inheritance:r /grant \`
        '*S-1-5-18:(OI)(CI)F' \`
        '*S-1-5-32-544:(OI)(CI)F' \`
        '*S-1-5-32-545:(OI)(CI)RX' | Out-Null

    if ($LASTEXITCODE -ne 0) {
        throw "Failed to set directory permissions. icacls exit code: $LASTEXITCODE."
    }

    & icacls.exe $file /inheritance:r /grant \`
        '*S-1-5-18:F' \`
        '*S-1-5-32-544:F' \`
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

    Remove-Item (Join-Path $dir 'managed-*.installed') \`
        -Force -ErrorAction SilentlyContinue

    New-Item -ItemType File \`
        -Path (Join-Path $dir "managed-$configRevision.installed") \`
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
`;
}

const startupScript = `# Starts the Speakeasy tray app at sign-in for every user. Runs as SYSTEM from Intune.
$ErrorActionPreference = 'Stop'

$startup = [Environment]::GetFolderPath('CommonStartup')
$shell = New-Object -ComObject WScript.Shell
$shortcut = $shell.CreateShortcut((Join-Path $startup 'Speakeasy.lnk'))
$shortcut.TargetPath = Join-Path $env:ProgramFiles 'Speakeasy\\speakeasy-ui.exe'
$shortcut.WorkingDirectory = Join-Path $env:ProgramFiles 'Speakeasy'
$shortcut.Save()
`;

// Version-aware detection that survives the msi's MajorUpgrade: any product
// under the agent's UpgradeCode at this version or newer counts as installed.
export function detectScript(version: string): string {
  const minimum = version.replace(/[-+].*$/, "");
  return `$minimum = [version]${psQuote(minimum)}
$installer = New-Object -ComObject WindowsInstaller.Installer
$type = $installer.GetType()
$products = $type.InvokeMember('RelatedProducts', 'GetProperty', $null, $installer, @(${psQuote(MSI_UPGRADE_CODE)}))
foreach ($product in $products) {
  $installed = [version]$type.InvokeMember('ProductInfo', 'GetProperty', $null, $installer, @($product, 'VersionString'))
  if ($installed -ge $minimum) {
    Write-Output "Speakeasy agent $installed installed"
    exit 0
  }
}
exit 1
`;
}

function readme(c: {
  version: string;
  msiFile: string;
  revision: string;
}): string {
  return `Speakeasy device agent for Microsoft Intune
===========================================

This bundle has three Win32 apps that are ready to upload. You don't need
IntuneWinAppUtil. In Intune, go to Apps > Windows > Add > Windows app (Win32)
and create them in this order. Use Install behavior "System" for all three.

The config app holds your org token. Treat its .intunewin like an API key.


1. Speakeasy Device Agent Config
   Package:     1-config/Deploy-SpeakeasyManagedJson.intunewin
   Install:     ${POWERSHELL} .\\${CONFIG_SCRIPT}
   Uninstall:   cmd.exe /c rmdir /s /q "%ProgramData%\\Speakeasy"
   Detection:   File. Path %ProgramData%\\Speakeasy
                File or folder managed-${c.revision}.installed
                Detection method: File or folder exists
   Assignments: none (installed as a dependency of app 3)

2. Speakeasy Device Agent Startup
   Package:     2-startup/Deploy-SpeakeasyStartup.intunewin
   Install:     ${POWERSHELL} .\\${STARTUP_SCRIPT}
   Uninstall:   cmd.exe /c del /q "%ProgramData%\\Microsoft\\Windows\\Start Menu\\Programs\\StartUp\\Speakeasy.lnk"
   Detection:   File. Path %ProgramData%\\Microsoft\\Windows\\Start Menu\\Programs\\StartUp
                File or folder Speakeasy.lnk
                Detection method: File or folder exists
   Assignments: none (installed as a dependency of app 3)

3. Speakeasy Device Agent (v${c.version})
   Package:     3-agent/${c.msiFile.replace(/\.msi$/, ".intunewin")}
   Install:     ${AGENT_INSTALL_SCRIPT}
   Uninstall:   msiexec /x ${c.msiFile} /qn /norestart
   Device restart behavior: No specific action
   Detection:   Use a custom detection script. Upload ${DETECT_SCRIPT}
                from this bundle. It finds v${c.version} or newer.
   Dependencies: add apps 1 and 2 with "Automatically install" set to Yes.
   Assignments: Required, for your device groups.


Rotating the org token: download a new bundle and add its config app as a
new Win32 app. Then swap it into app 3's dependencies. The new detection file
name makes Intune install it again.

New agent versions: download a new bundle and upload its agent package to
app 3. Then change the uninstall command to the new msi file name, and
replace the detection script with the new one.
`;
}

// The agent package's setup file is a .cmd rather than the msi. With an msi
// setup file Intune expects the MsiInfo block IntuneWinAppUtil reads out of
// the msi database, which we don't parse here.
function agentInstallScript(msiFile: string): string {
  return `@echo off\r\nmsiexec /i "%~dp0${msiFile}" /qn /norestart\r\nexit /b %errorlevel%\r\n`;
}

export type IntuneBundleInput = IntuneConfig & {
  version: string;
  msi: Uint8Array;
};

export async function buildIntuneBundle(
  input: IntuneBundleInput,
): Promise<Uint8Array> {
  const enc = new TextEncoder();
  // Windows PowerShell 5.1 reads a BOM-less script as ANSI.
  const ps1 = (script: string) => enc.encode(`\uFEFF${script}`);
  const msiFile = `speakeasy-agent_${input.version}.msi`;

  const [config, startup, agent] = await Promise.all([
    buildIntuneWin({
      setupFile: CONFIG_SCRIPT,
      files: { [CONFIG_SCRIPT]: ps1(configScript(input)) },
    }),
    buildIntuneWin({
      setupFile: STARTUP_SCRIPT,
      files: { [STARTUP_SCRIPT]: ps1(startupScript) },
    }),
    buildIntuneWin({
      setupFile: AGENT_INSTALL_SCRIPT,
      files: {
        [AGENT_INSTALL_SCRIPT]: enc.encode(agentInstallScript(msiFile)),
        [msiFile]: input.msi,
      },
    }),
  ]);

  return zipSync(
    {
      "README.txt": enc.encode(
        readme({ version: input.version, msiFile, revision: input.revision }),
      ),
      [DETECT_SCRIPT]: ps1(detectScript(input.version)),
      "1-config/Deploy-SpeakeasyManagedJson.intunewin": config,
      "2-startup/Deploy-SpeakeasyStartup.intunewin": startup,
      [`3-agent/${msiFile.replace(/\.msi$/, ".intunewin")}`]: agent,
    },
    { level: 0 },
  );
}
