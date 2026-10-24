import { describe, expect, it } from "vitest";
import {
  headerDraftProblem,
  headerDraftsFrom,
  headerInputs,
  keepsStoredValue,
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
    expect(keepsStoredValue(draft!)).toBe(true);
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

  it("keeps a stored non-secret value and its description", () => {
    const [draft] = headerDraftsFrom([
      {
        name: "X-Api-Version",
        description: "Pinned upstream API version",
        isSecret: false,
        isRequired: false,
        hasStoredValue: true,
      },
    ]);
    expect(headerDraftProblem("static_credential", [draft!])).toBeNull();
    expect(headerInputs([draft!])).toEqual([
      {
        name: "X-Api-Version",
        description: "Pinned upstream API version",
        isSecret: false,
        isRequired: false,
        value: undefined,
        valueFromRequestHeader: undefined,
      },
    ]);
  });

  it("asks for a new value once a stored header is renamed", () => {
    const [draft] = headerDraftsFrom([
      {
        name: "X-Api-Key",
        isSecret: true,
        isRequired: false,
        hasStoredValue: true,
      },
    ]);
    const renamed = { ...draft!, name: "X-Other-Key" };
    expect(keepsStoredValue(renamed)).toBe(false);
    expect(headerDraftProblem("static_credential", [renamed])).toMatch(
      /stored value stays with the name “X-Api-Key”/,
    );
    expect(
      headerDraftProblem("static_credential", [
        { ...renamed, name: "x_api_key" },
      ]),
    ).toBeNull();
    expect(
      headerDraftProblem("static_credential", [{ ...renamed, value: "new" }]),
    ).toBeNull();
  });

  it("refuses reserved headers before they are sent", () => {
    const forward = (name: string, fromHeader: string) => ({
      ...newHeaderDraft("forwarded", name),
      fromHeader,
    });
    for (const source of [
      "Authorization",
      "cookie",
      "Set-Cookie",
      "proxy_authorization",
    ]) {
      expect(
        headerDraftProblem("pass_through", [forward("X-Upstream", source)]),
      ).toMatch(/never forwarded upstream/);
    }
    expect(
      headerDraftProblem("pass_through", [forward("Cookie", "X-Session")]),
    ).toMatch(/Cookie can only hold a stored value/);
    for (const name of ["Set-Cookie", "Proxy-Authorization"]) {
      expect(
        headerDraftProblem("static_credential", [
          { ...newHeaderDraft("stored", name), value: "x" },
        ]),
      ).toMatch(/is reserved/);
    }
    expect(
      headerDraftProblem("static_credential", [
        { ...newHeaderDraft("stored", "Cookie"), value: "session=x" },
      ]),
    ).toBeNull();
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
