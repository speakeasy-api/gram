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
  return `# Writes the Speakeasy device agent identity file. Runs as SYSTEM from Intune.
$ErrorActionPreference = 'Stop'

$orgToken = ${psQuote(c.orgToken)}
$orgSlug = ${psQuote(c.orgSlug)}
$orgName = ${psQuote(c.orgName)}
$autoUpdate = ${psQuote(c.autoUpdate)}
$configRevision = ${psQuote(c.revision)}

function Test-Upn([string]$upn) {
  if (-not $upn) { return $false }
  if ($upn -notmatch '^[^@\\s]+@[^@\\s]+\\.[^@\\s]+$') { return $false }
  return $upn -notmatch '^(foouser|autopilot)@'
}

function Get-UserEmail {
  foreach ($key in Get-ChildItem 'HKLM:\\SOFTWARE\\Microsoft\\Enrollments' -ErrorAction SilentlyContinue) {
    $upn = (Get-ItemProperty -Path $key.PSPath -ErrorAction SilentlyContinue).UPN
    if (Test-Upn $upn) { return $upn }
  }

  $account = (Get-CimInstance Win32_ComputerSystem).UserName
  if ($account) {
    try {
      $sid = (New-Object System.Security.Principal.NTAccount($account)).Translate([System.Security.Principal.SecurityIdentifier]).Value
      $cache = "HKLM:\\SOFTWARE\\Microsoft\\IdentityStore\\Cache\\$sid\\IdentityCache\\$sid"
      $upn = (Get-ItemProperty -Path $cache -ErrorAction SilentlyContinue).UserName
      if (Test-Upn $upn) { return $upn }
    } catch {}
  }

  foreach ($key in Get-ChildItem 'HKLM:\\SYSTEM\\CurrentControlSet\\Control\\CloudDomainJoin\\JoinInfo' -ErrorAction SilentlyContinue) {
    $upn = (Get-ItemProperty -Path $key.PSPath -ErrorAction SilentlyContinue).UserEmail
    if (Test-Upn $upn) { return $upn }
  }

  return $null
}

$email = Get-UserEmail
if (-not $email) {
  Write-Error 'No user email found yet. Intune retries this install after a user signs in.'
  exit 1
}

$dir = Join-Path $env:ProgramData 'Speakeasy'
New-Item -ItemType Directory -Force -Path $dir | Out-Null

$config = [ordered]@{
  v = 1
  email = $email.ToLowerInvariant()
  org_token = $orgToken
  org_slug = $orgSlug
  org_name = $orgName
  auto_update = $autoUpdate
}
$path = Join-Path $dir 'managed.json'
[System.IO.File]::WriteAllText($path, ($config | ConvertTo-Json), (New-Object System.Text.UTF8Encoding $false))

# The agent runs as the signed-in user, so Users need read access.
& icacls.exe $dir /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' '*S-1-5-32-545:(OI)(CI)RX' | Out-Null
if ($LASTEXITCODE -ne 0) { Write-Error "icacls failed on $dir ($LASTEXITCODE)"; exit 1 }
& icacls.exe $path /inheritance:r /grant:r '*S-1-5-18:F' '*S-1-5-32-544:F' '*S-1-5-32-545:R' | Out-Null
if ($LASTEXITCODE -ne 0) { Write-Error "icacls failed on $path ($LASTEXITCODE)"; exit 1 }

$daemon = Join-Path $env:ProgramFiles 'Speakeasy\\speakeasyd.exe'
if (Test-Path $daemon) {
  & $daemon -service restart
  if ($LASTEXITCODE -ne 0) { Write-Error "speakeasyd restart failed ($LASTEXITCODE)"; exit 1 }
}

Get-ChildItem -Path $dir -Filter 'managed-*.installed' | Remove-Item -Force
New-Item -ItemType File -Force -Path (Join-Path $dir "managed-$configRevision.installed") | Out-Null
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
app 3. Then replace the detection script with the new one.
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
