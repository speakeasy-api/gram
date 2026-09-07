import type { IssuerFieldMismatch } from "@gram/client/models/components/issuerfieldmismatch.js";
import { describe, expect, it } from "vitest";
import {
  isListMismatch,
  listMismatchDelta,
  mismatchFieldNames,
  mismatchValueLabel,
  warningSentence,
} from "./issuerMismatches";

function listMismatch(
  sourceValues: string[] | undefined,
  targetValues: string[] | undefined,
): IssuerFieldMismatch {
  return { field: "scopes_supported", sourceValues, targetValues };
}

describe("mismatchFieldNames", () => {
  it("keeps the server's order", () => {
    expect(
      mismatchFieldNames([
        { field: "issuer" },
        { field: "token_endpoint" },
        { field: "authorization_endpoint" },
      ]),
    ).toEqual(["issuer", "token_endpoint", "authorization_endpoint"]);
  });
});

describe("mismatchValueLabel", () => {
  it("renders a value as itself", () => {
    expect(mismatchValueLabel("https://idp.example.com/token")).toBe(
      "https://idp.example.com/token",
    );
  });

  // The server blocks a migration when one side declares an endpoint and the
  // other does not, so "declares nothing" has to read differently from
  // "declares an empty value" — and neither may render as a blank.
  it("distinguishes an unset value from an empty one", () => {
    expect(mismatchValueLabel(undefined)).toBe("not set");
    expect(mismatchValueLabel("")).toBe("empty");
  });
});

describe("isListMismatch", () => {
  it("recognizes entries on either side alone", () => {
    expect(isListMismatch(listMismatch(["openid"], undefined))).toBe(true);
    expect(isListMismatch(listMismatch(undefined, ["openid"]))).toBe(true);
  });

  // An empty list arrives as an absent field, which is indistinguishable from a
  // scalar whose two sides are both unset. Neither has a delta to draw.
  it("treats two empty sides as scalar", () => {
    expect(isListMismatch(listMismatch(undefined, undefined))).toBe(false);
  });

  it("does not mistake a scalar for a list", () => {
    expect(
      isListMismatch({
        field: "oidc",
        sourceValue: "false",
        targetValue: "true",
      }),
    ).toBe(false);
  });
});

describe("listMismatchDelta", () => {
  it("reports what migrated clients gain and lose", () => {
    expect(
      listMismatchDelta(
        listMismatch(["openid", "email"], ["openid", "profile"]),
      ),
    ).toEqual({ added: ["profile"], removed: ["email"] });
  });

  it("reports an empty side as a pure gain", () => {
    expect(listMismatchDelta(listMismatch(undefined, ["openid"]))).toEqual({
      added: ["openid"],
      removed: [],
    });
  });

  // The server compares list fields as sets and so no longer reports a
  // difference that adds and removes nothing. It owns that comparison, though,
  // and this is what backs the caller's fallback to showing both lists whole.
  it("comes back empty when neither side holds an entry the other lacks", () => {
    expect(
      listMismatchDelta(listMismatch(["openid", "openid"], ["openid"])),
    ).toEqual({ added: [], removed: [] });
  });

  // A repeat alongside a genuine change must not hide the change, and must not
  // report the repeated entry as lost when both sides still offer it.
  it("ignores a repeat when a real entry also changed", () => {
    expect(
      listMismatchDelta(
        listMismatch(["openid", "openid", "email"], ["openid", "profile"]),
      ),
    ).toEqual({ added: ["profile"], removed: ["email"] });
  });
});

describe("warningSentence", () => {
  it("names both sides of a scalar change", () => {
    expect(
      warningSentence({
        field: "oidc",
        sourceValue: "false",
        targetValue: "true",
      }),
    ).toBe("oidc changes from false to true for migrated clients.");
  });

  // A list's values are shown as a delta beneath the sentence, so the sentence
  // itself says only who wins.
  it("defers a list's values to the delta", () => {
    expect(warningSentence(listMismatch(["openid"], ["openid", "email"]))).toBe(
      "scopes_supported differs; the target provider's values become authoritative.",
    );
  });
});
