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
  it("embeds the org settings as quoted literals", () => {
    const script = configScript({ ...config, orgName: "O'Brien & Co" });
    expect(script).toContain("$orgToken = 'spk_org_test'");
    expect(script).toContain("$orgSlug = 'example-corp'");
    expect(script).toContain("$orgName = 'O''Brien & Co'");
    expect(script).toContain("$autoUpdate = 'notify'");
    expect(script).toContain("$configRevision = '20261119184500'");
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
    const readme = strFromU8(files["README.txt"]);
    expect(readme).toContain("managed-20261119184500.installed");
    expect(readme).toContain("Install-SpeakeasyAgent.cmd");
    expect(files["Detect-SpeakeasyAgent.ps1"].slice(0, 3)).toEqual(
      new Uint8Array([0xef, 0xbb, 0xbf]),
    );
  });
});
