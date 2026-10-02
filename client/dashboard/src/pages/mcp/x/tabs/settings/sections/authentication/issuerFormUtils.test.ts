import { CreateRemoteSessionClientFormTokenEndpointAuthMethod as AuthMethod } from "@gram/client/models/components/createremotesessionclientform.js";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  availableClientTypes,
  clientSecretUpdateValue,
  dynamicClientRegistrationAvailability,
  legacyCallbackURL,
  remoteLoginCallbackURL,
} from "./issuerFormUtils";

const server = vi.hoisted(() => ({ url: "https://app.example.com" }));

vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  getServerURL: () => server.url,
}));

afterEach(() => {
  server.url = "https://app.example.com";
});

describe("callback URLs", () => {
  it("appends the callback paths to the server URL", () => {
    expect(remoteLoginCallbackURL()).toBe(
      "https://app.example.com/mcp/remote_login_callback",
    );
    expect(legacyCallbackURL()).toBe("https://app.example.com/oauth/callback");
  });

  it("drops a trailing slash the way the server does", () => {
    server.url = "https://app.example.com/";

    expect(remoteLoginCallbackURL()).toBe(
      "https://app.example.com/mcp/remote_login_callback",
    );
    expect(legacyCallbackURL()).toBe("https://app.example.com/oauth/callback");
  });
});

describe("clientSecretUpdateValue", () => {
  it("does not rotate an unsaved secret for private_key_jwt", () => {
    expect(
      clientSecretUpdateValue(AuthMethod.PrivateKeyJwt, "new-secret"),
    ).toBe(undefined);
  });

  it("forwards a new secret for secret-based authentication", () => {
    expect(
      clientSecretUpdateValue(AuthMethod.ClientSecretBasic, " new-secret "),
    ).toBe("new-secret");
    expect(clientSecretUpdateValue(AuthMethod.ClientSecretPost, "  ")).toBe(
      undefined,
    );
  });
});

describe("dynamicClientRegistrationAvailability", () => {
  it("keeps direct DCR available to non-platform admins", () => {
    expect(
      dynamicClientRegistrationAvailability({
        registrationEndpoint: "https://idp.example/register",
        tunneled: false,
        isPlatformAdmin: false,
      }),
    ).toEqual({ available: true, permissionRestricted: false });
  });

  it("restricts tunneled DCR for non-platform admins", () => {
    expect(
      dynamicClientRegistrationAvailability({
        registrationEndpoint: "https://idp.example/register",
        tunneled: true,
        isPlatformAdmin: false,
      }),
    ).toEqual({ available: false, permissionRestricted: true });
  });

  it("keeps tunneled DCR available to platform admins", () => {
    expect(
      dynamicClientRegistrationAvailability({
        registrationEndpoint: "https://idp.example/register",
        tunneled: true,
        isPlatformAdmin: true,
      }),
    ).toEqual({ available: true, permissionRestricted: false });
  });
});

describe("availableClientTypes", () => {
  it.each([
    {
      capabilities: { cimdAvailable: true, dcrAvailable: true },
      expected: ["cimd", "dcr", "manual"],
    },
    {
      capabilities: { cimdAvailable: true, dcrAvailable: false },
      expected: ["cimd", "manual"],
    },
    {
      capabilities: { cimdAvailable: false, dcrAvailable: true },
      expected: ["dcr", "manual"],
    },
    {
      capabilities: { cimdAvailable: false, dcrAvailable: false },
      expected: ["manual"],
    },
  ])("orders $expected for $capabilities", ({ capabilities, expected }) => {
    expect(availableClientTypes(capabilities)).toEqual(expected);
  });
});
