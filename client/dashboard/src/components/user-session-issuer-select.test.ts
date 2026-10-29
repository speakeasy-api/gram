import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { describe, expect, it } from "vitest";
import {
  PROJECT_SPECIFIC_ISSUER_VALUE,
  defaultCreationUserSessionIssuerValue,
  userSessionIssuerLabel,
} from "./user-session-issuer-select.utils";

function issuer(id: string): UserSessionIssuer {
  return { id } as UserSessionIssuer;
}

describe("defaultCreationUserSessionIssuerValue", () => {
  it("uses the project-specific fallback when no organization issuer exists", () => {
    expect(defaultCreationUserSessionIssuerValue([])).toBe(
      PROJECT_SPECIFIC_ISSUER_VALUE,
    );
  });

  it("preselects the sole organization issuer", () => {
    expect(defaultCreationUserSessionIssuerValue([issuer("org-issuer")])).toBe(
      "org-issuer",
    );
  });

  it("requires an explicit choice when several organization issuers exist", () => {
    expect(
      defaultCreationUserSessionIssuerValue([
        issuer("org-issuer-a"),
        issuer("org-issuer-b"),
      ]),
    ).toBe("");
  });
});

describe("userSessionIssuerLabel", () => {
  const base = {
    id: "issuer-1",
    slug: "workforce",
    projectId: "",
    authorizationServerMode: "shared",
  } as UserSessionIssuer;

  it("marks an issuer that serves a shared authorization server", () => {
    expect(
      userSessionIssuerLabel({
        ...base,
        issuerUrl: "https://app.example.com/oauth/usi/issuer-1",
      }),
    ).toBe("workforce — Organization · Shared authorization server");
  });

  it("does not mark a shared issuer whose authorization server is not served", () => {
    expect(
      userSessionIssuerLabel({
        ...base,
        authorizationServerUnavailableReason: "host_not_served",
      }),
    ).toBe("workforce — Organization");
  });

  it("names a project issuer's owner", () => {
    expect(
      userSessionIssuerLabel({
        ...base,
        projectId: "project-1",
        authorizationServerMode: "endpoint",
      }),
    ).toBe("workforce — Project");
  });
});
