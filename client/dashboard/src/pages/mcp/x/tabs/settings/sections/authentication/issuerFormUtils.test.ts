import { describe, expect, it } from "vitest";

import { dynamicClientRegistrationAvailability } from "./issuerFormUtils";

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
