import { describe, expect, it } from "vitest";
import { approvalsServerSetupNote } from "./approvals-server-note";

describe("approvalsServerSetupNote", () => {
  const setup = {
    mcpServerId: "00000000-0000-0000-0000-000000000001",
    mcpServerSlug: "speakeasy-approvals-0001",
    pluginServerName: "speakeasy-approvals",
    addedToDefaultPlugin: true,
  };

  it("says nothing when the save set up no server", () => {
    expect(approvalsServerSetupNote(undefined)).toBeUndefined();
  });

  it("names the server and the Default plugin it joined", () => {
    expect(approvalsServerSetupNote(setup)).toBe(
      "Added speakeasy-approvals to the Default plugin, which republishes so agents can request approval.",
    );
  });

  it("asks for the server to be added when no Default plugin took it", () => {
    expect(
      approvalsServerSetupNote({ ...setup, addedToDefaultPlugin: false }),
    ).toContain("Add it to the plugins your agents use.");
  });
});
