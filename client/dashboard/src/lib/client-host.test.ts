import { describe, expect, it } from "vitest";
import { clientServerURLOverride, type SpeakeasyClient } from "./client-host";

const US_HOSTS: [string, ...string[]] = ["app.getgram.ai", "ai.speakeasy.com"];
// Typed as a record so adding a client without covering it here fails
// type-checking.
const CLIENTS = Object.keys({
  cli: true,
  hooks: true,
  deviceAgent: true,
} satisfies Record<SpeakeasyClient, true>) as SpeakeasyClient[];

describe("clientServerURLOverride", () => {
  it.each(
    CLIENTS.flatMap((client) =>
      ["https://app.getgram.ai", "https://ai.speakeasy.com"].map(
        (serverURL) => [client, serverURL] as const,
      ),
    ),
  )("leaves %s on its default when served from %s", (client, serverURL) => {
    expect(
      clientServerURLOverride(client, serverURL, {
        firstPartyHosts: US_HOSTS,
      }),
    ).toBeNull();
  });

  it.each(CLIENTS)("names another partition's host for %s", (client) => {
    expect(
      clientServerURLOverride(client, "https://ai.eu.example.com/", {
        firstPartyHosts: ["ai.eu.example.com"],
      }),
    ).toBe("https://ai.eu.example.com");
  });

  it("names a dev host, whose deployment does not serve the default", () => {
    expect(
      clientServerURLOverride("hooks", "https://dev.ai.speakeasy.com", {
        firstPartyHosts: ["dev.getgram.ai", "dev.ai.speakeasy.com"],
      }),
    ).toBe("https://dev.ai.speakeasy.com");
  });

  it("keeps the port of a local stack", () => {
    expect(clientServerURLOverride("cli", "https://localhost:8080", {})).toBe(
      "https://localhost:8080",
    );
  });

  it("ignores configured hosts that do not include the serving host", () => {
    expect(
      clientServerURLOverride("deviceAgent", "https://preview.example.com", {
        firstPartyHosts: US_HOSTS,
      }),
    ).toBe("https://preview.example.com");
  });

  it.each(["https://app.getgram.ai:8443", "http://app.getgram.ai"])(
    "names %s, which the default's scheme and port do not reach",
    (serverURL) => {
      expect(
        clientServerURLOverride("hooks", serverURL, {
          firstPartyHosts: US_HOSTS,
        }),
      ).toBe(serverURL);
    },
  );

  it("leaves the default host alone without runtime configuration", () => {
    expect(
      clientServerURLOverride("deviceAgent", "https://app.getgram.ai", {}),
    ).toBeNull();
  });
});
