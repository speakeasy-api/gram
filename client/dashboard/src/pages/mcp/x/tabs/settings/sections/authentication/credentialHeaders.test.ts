import { describe, expect, it } from "vitest";
import {
  headerDraftProblem,
  headerDraftsFrom,
  headerInputs,
  newHeaderDraft,
} from "./credentialHeaders";

describe("credential header drafts", () => {
  it("keeps a stored secret when no new value is entered", () => {
    const [draft] = headerDraftsFrom([
      {
        name: "X-Api-Key",
        isSecret: true,
        isRequired: false,
        hasStoredValue: true,
      },
    ]);
    expect(draft?.keepsStored).toBe(true);
    expect(headerDraftProblem("static_credential", [draft!])).toBeNull();
    expect(headerInputs([draft!])).toEqual([
      {
        name: "X-Api-Key",
        isSecret: true,
        isRequired: false,
        value: undefined,
        valueFromRequestHeader: undefined,
      },
    ]);
  });

  it("refuses an Authorization header on a credential that signs in", () => {
    const draft = { ...newHeaderDraft("stored", "authorization"), value: "x" };
    expect(headerDraftProblem("service_account", [draft])).toMatch(
      /Sign-in produces the Authorization header/,
    );
    expect(headerDraftProblem("static_credential", [draft])).toBeNull();
  });

  it("matches header names regardless of case and underscores", () => {
    const first = { ...newHeaderDraft("stored", "X-Key"), value: "a" };
    const second = { ...newHeaderDraft("stored", "x_key"), value: "b" };
    expect(headerDraftProblem("static_credential", [first, second])).toMatch(
      /listed twice/,
    );
  });

  it("needs the caller's header for a forwarded header", () => {
    const draft = newHeaderDraft("forwarded", "Authorization");
    expect(headerDraftProblem("pass_through", [draft])).toMatch(/copied from/);
    expect(
      headerInputs([{ ...draft, fromHeader: " X-Upstream-Token " }]),
    ).toEqual([
      {
        name: "Authorization",
        isSecret: false,
        isRequired: true,
        value: undefined,
        valueFromRequestHeader: "X-Upstream-Token",
      },
    ]);
  });

  it("needs at least one header on a credential made of headers", () => {
    expect(headerDraftProblem("static_credential", [])).toMatch(/at least one/);
    expect(headerDraftProblem("service_account", [])).toBeNull();
  });
});
