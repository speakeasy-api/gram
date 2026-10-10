import { describe, expect, it } from "vitest";
import { scopeLabels } from "./scopePrefix";

const GOOGLE = "https://www.googleapis.com/auth/";

// Labels as a plain object, for readable expectations.
const labels = (scopes: string[]) => Object.fromEntries(scopeLabels(scopes));

describe("scopeLabels", () => {
  it("labels scopes under the URL base most share by what follows it", () => {
    expect(
      labels([
        "openid",
        `${GOOGLE}calendar`,
        `${GOOGLE}drive.readonly`,
        `${GOOGLE}gmail.readonly`,
        "https://other.example.com/scope/a",
      ]),
    ).toEqual({
      openid: "openid",
      [`${GOOGLE}calendar`]: "calendar",
      [`${GOOGLE}drive.readonly`]: "drive.readonly",
      [`${GOOGLE}gmail.readonly`]: "gmail.readonly",
      "https://other.example.com/scope/a": "https://other.example.com/scope/a",
    });
  });

  it("labels every scope whole when fewer than three share a base", () => {
    expect(labels([`${GOOGLE}calendar`, `${GOOGLE}drive`])).toEqual({
      [`${GOOGLE}calendar`]: `${GOOGLE}calendar`,
      [`${GOOGLE}drive`]: `${GOOGLE}drive`,
    });
    expect(labels(["read", "write", "admin"])).toEqual({
      read: "read",
      write: "write",
      admin: "admin",
    });
  });

  it("never treats a bare origin as a base", () => {
    expect(labels(["https://a.com", "https://b.com", "https://c.com"])).toEqual(
      {
        "https://a.com": "https://a.com",
        "https://b.com": "https://b.com",
        "https://c.com": "https://c.com",
      },
    );
  });

  it("keeps the base itself whole", () => {
    expect(
      labels([GOOGLE, `${GOOGLE}calendar`, `${GOOGLE}drive`])[GOOGLE],
    ).toBe(GOOGLE);
  });

  it("keeps the whole scope when its short form is another scope", () => {
    expect(
      labels([
        "drive",
        `${GOOGLE}calendar`,
        `${GOOGLE}drive`,
        `${GOOGLE}gmail`,
      ]),
    ).toEqual({
      drive: "drive",
      [`${GOOGLE}calendar`]: "calendar",
      [`${GOOGLE}drive`]: `${GOOGLE}drive`,
      [`${GOOGLE}gmail`]: "gmail",
    });
  });
});
