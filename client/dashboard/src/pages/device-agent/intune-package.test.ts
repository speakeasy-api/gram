// @vitest-environment node
import { strFromU8, unzipSync } from "fflate";
import { describe, expect, it } from "vitest";
import {
  buildIntuneBundle,
  configRevision,
  configScript,
  detectScript,
  psQuote,
} from "./intune-package";

function entry(files: Record<string, Uint8Array>, name: string): Uint8Array {
  const file = files[name];
  if (!file) throw new Error(`missing ${name}`);
  return file;
}

const tag = (xml: string, name: string) =>
  new RegExp(`<${name}>(.*?)</${name}>`).exec(xml)?.[1] ?? "";
const b64 = (s: string) => Uint8Array.from(atob(s), (c) => c.charCodeAt(0));

async function openPackage(pkg: Uint8Array) {
  const outer = unzipSync(pkg);
  const xml = strFromU8(
    entry(outer, "IntuneWinPackage/Metadata/Detection.xml"),
  );
  const blob = entry(
    outer,
    "IntuneWinPackage/Contents/IntunePackage.intunewin",
  );
  const key = await globalThis.crypto.subtle.importKey(
    "raw",
    b64(tag(xml, "EncryptionKey")),
    { name: "AES-CBC" },
    false,
    ["decrypt"],
  );
  const plain = await globalThis.crypto.subtle.decrypt(
    { name: "AES-CBC", iv: blob.slice(32, 48) },
    key,
    blob.slice(48),
  );
  return {
    setupFile: tag(xml, "SetupFile"),
    files: unzipSync(new Uint8Array(plain)),
  };
}

const config = {
  orgToken: "spk_org_test",
  orgSlug: "example-corp",
  orgName: "Example Corp",
  autoUpdate: "notify" as const,
  revision: "20261119184500",
};

describe("psQuote", () => {
  it("doubles straight and typographic single quotes", () => {
    expect(psQuote("it's")).toBe("'it''s'");
    expect(psQuote("‘a’")).toBe("'‘‘a’’'");
    expect(psQuote("$(whoami)")).toBe("'$(whoami)'");
  });
});

describe("configScript", () => {
  it.each(["notify", "automatic"] as const)(
    "fills the manual JSON template in %s mode",
    (autoUpdate) => {
      const orgName = `O'Brien “HQ” "West" \\ $(whoami) {{mail}}\n'@`;
      const script = configScript({ ...config, orgName, autoUpdate });
      const template = /\$template = @'\n([\s\S]*?)\n'@/.exec(script)?.[1];
      expect(JSON.parse(template ?? "")).toEqual({
        v: 1,
        email: "{{mail}}",
        org_token: config.orgToken,
        org_slug: config.orgSlug,
        org_name: orgName,
        auto_update: autoUpdate,
      });
      expect(script).toContain("$configRevision = '20261119184500'");
      expect(script).toContain("$config.email = Get-EnrolledUserEmail");
      expect(script).toContain("managed-$configRevision.installed");
    },
  );

  it("prefers the enrollment UPN before trying the interactive identity sources", () => {
    const script = configScript(config);
    const lookup = script.split("function Get-EnrolledUserEmail {")[1];
    expect(lookup).toMatch(
      /HKLM:\\SOFTWARE\\Microsoft\\Enrollments[\s\S]*?\.UPN[\s\S]*?return \(Get-ActiveEntraUserEmail\)/,
    );
    const fallback = script.split("function Get-ActiveEntraUserEmail {")[1];
    expect(fallback).toMatch(
      /AzureAD[\s\S]*?Get-AadNgcUserId -Sid \$user.Sid[\s\S]*?Get-DomainUserPrincipalName -Sid \$user.Sid/,
    );
    expect(script).toContain("WorkplaceJoin\\AADNGC");
    expect(script).toContain("UserPrincipal]::FindByIdentity");
  });
});

describe("configRevision", () => {
  it("is a sortable UTC timestamp", () => {
    expect(configRevision(new Date("2026-11-19T18:45:00.123Z"))).toBe(
      "20261119184500",
    );
  });
});

describe("detectScript", () => {
  it("drops prerelease suffixes, which msi versions can't carry", () => {
    expect(detectScript("1.2.3-beta.1")).toContain("[version]'1.2.3'");
  });
});

describe("buildIntuneBundle", () => {
  it("lays out three packages, the detection script and a README", async () => {
    const bundle = await buildIntuneBundle({
      ...config,
      version: "1.2.3",
      msi: new Uint8Array([1, 2, 3]),
    });
    const files = unzipSync(bundle);
    expect(Object.keys(files).sort()).toEqual([
      "1-config/Deploy-SpeakeasyManagedJson.intunewin",
      "2-startup/Deploy-SpeakeasyStartup.intunewin",
      "3-agent/speakeasy-agent_1.2.3.intunewin",
      "Detect-SpeakeasyAgent.ps1",
      "README.txt",
    ]);
    const readme = strFromU8(entry(files, "README.txt"));
    expect(readme).toContain("managed-20261119184500.installed");
    expect(readme).toContain("Install-SpeakeasyAgent.cmd");
    const configPkg = await openPackage(
      entry(files, "1-config/Deploy-SpeakeasyManagedJson.intunewin"),
    );
    expect(configPkg.setupFile).toBe("Deploy-SpeakeasyManagedJson.ps1");
    expect(entry(configPkg.files, "Deploy-SpeakeasyManagedJson.ps1")).toEqual(
      new TextEncoder().encode(`\uFEFF${configScript(config)}`),
    );

    const startupPkg = await openPackage(
      entry(files, "2-startup/Deploy-SpeakeasyStartup.intunewin"),
    );
    expect(startupPkg.setupFile).toBe("Deploy-SpeakeasyStartup.ps1");
    expect(
      strFromU8(entry(startupPkg.files, "Deploy-SpeakeasyStartup.ps1")),
    ).toContain("speakeasy-ui.exe");

    const agentPkg = await openPackage(
      entry(files, "3-agent/speakeasy-agent_1.2.3.intunewin"),
    );
    expect(agentPkg.setupFile).toBe("Install-SpeakeasyAgent.cmd");
    expect(entry(agentPkg.files, "speakeasy-agent_1.2.3.msi")).toEqual(
      new Uint8Array([1, 2, 3]),
    );
    expect(
      strFromU8(entry(agentPkg.files, "Install-SpeakeasyAgent.cmd")),
    ).toContain('msiexec /i "%~dp0speakeasy-agent_1.2.3.msi" /qn /norestart');
    expect(entry(files, "Detect-SpeakeasyAgent.ps1").slice(0, 3)).toEqual(
      new Uint8Array([0xef, 0xbb, 0xbf]),
    );
  });
});
