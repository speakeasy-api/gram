import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { describe, expect, it } from "vitest";
import {
  PROJECT_SPECIFIC_ISSUER_VALUE,
  defaultCreationUserSessionIssuerValue,
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
