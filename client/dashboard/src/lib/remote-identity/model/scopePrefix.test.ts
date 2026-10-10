import { describe, expect, it } from "vitest";
import { sharedScopePrefix, shortScope } from "./scopePrefix";

const GOOGLE = "https://www.googleapis.com/auth/";

describe("sharedScopePrefix", () => {
  it("finds the URL base most scopes share", () => {
    expect(
      sharedScopePrefix([
        "openid",
        `${GOOGLE}calendar`,
        `${GOOGLE}drive.readonly`,
        `${GOOGLE}gmail.readonly`,
        "https://other.example.com/scope/a",
      ]),
    ).toBe(GOOGLE);
  });

  it("leaves short lists and plain scopes alone", () => {
    expect(sharedScopePrefix([`${GOOGLE}calendar`, `${GOOGLE}drive`])).toBe("");
    expect(sharedScopePrefix(["read", "write", "admin"])).toBe("");
  });

  it("never treats a bare origin as a base", () => {
    expect(
      sharedScopePrefix(["https://a.com", "https://b.com", "https://c.com"]),
    ).toBe("");
  });
});

describe("shortScope", () => {
  it("drops the base only when something follows it", () => {
    expect(shortScope(`${GOOGLE}calendar`, GOOGLE)).toBe("calendar");
    expect(shortScope(GOOGLE, GOOGLE)).toBe(GOOGLE);
    expect(shortScope("openid", GOOGLE)).toBe("openid");
    expect(shortScope("openid", "")).toBe("openid");
  });
});
