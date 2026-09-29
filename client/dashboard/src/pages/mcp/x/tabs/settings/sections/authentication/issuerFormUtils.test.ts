import { CreateRemoteSessionClientFormTokenEndpointAuthMethod as AuthMethod } from "@gram/client/models/components/createremotesessionclientform.js";
import { describe, expect, it } from "vitest";

import {
  availableClientTypes,
  clientSecretUpdateValue,
  dynamicClientRegistrationAvailability,
} from "./issuerFormUtils";

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
