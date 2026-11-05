import { describe, expect, it } from "vitest";
import {
  remoteSessionClientDisplayName,
  remoteSessionClientShortId,
} from "./clientDisplay";

describe("remoteSessionClientDisplayName", () => {
  it("prefers the display name", () => {
    expect(
      remoteSessionClientDisplayName({
        id: "row-1",
        clientId: "upstream-client",
        displayName: "Production login",
      }),
    ).toBe("Production login");
  });

  it("falls back to the client ID", () => {
    expect(
      remoteSessionClientDisplayName({
        id: "row-1",
        clientId: "upstream-client",
      }),
    ).toBe("upstream-client");
  });

  it("falls back to the row ID for a CIMD client", () => {
    expect(
      remoteSessionClientDisplayName({
        id: "row-1",
        clientId: "https://app.example.com/cimd/row-1",
        clientIdMetadataUri: "https://app.example.com/cimd/row-1",
      }),
    ).toBe("row-1");
  });
});

describe("remoteSessionClientShortId", () => {
  it("shows a named CIMD client's row ID, not its metadata URL", () => {
    expect(
      remoteSessionClientShortId({
        id: "row-1",
        clientId: "https://app.example.com/cimd/row-1",
        clientIdMetadataUri: "https://app.example.com/cimd/row-1",
      }),
    ).toBe("row-1");
  });

  it("shows any other client's client ID", () => {
    expect(
      remoteSessionClientShortId({ id: "row-1", clientId: "upstream-client" }),
    ).toBe("upstream-client");
  });
});
