import { describe, expect, it } from "vitest";

import {
  appInstanceOptions,
  AUDIENCE_MAX_RUNES,
  buildConfirmRequests,
  clientBindingNote,
  isConfirmable,
  normalizeAudience,
  notApplicableReasonLabel,
  notApplicableReasonSummary,
  XAA_CONFIRM_BATCH,
  xaaStateLabel,
  xaaStateVariant,
} from "./xaaView";

describe("appInstanceOptions", () => {
  it("appends the Okta app id only to duplicate labels", () => {
    expect(
      appInstanceOptions([
        { oktaAppId: "0oa1", label: "Onboarding" },
        { oktaAppId: "0oa2", label: "Onboarding" },
        { oktaAppId: "0oa3", label: "Linear" },
      ]),
    ).toEqual([
      { id: "0oa1", label: "Onboarding · 0oa1" },
      { id: "0oa2", label: "Onboarding · 0oa2" },
      { id: "0oa3", label: "Linear" },
    ]);
  });
});

describe("notApplicableReasonSummary", () => {
  it("keeps the cell copy short while the label carries the full sentence", () => {
    expect(notApplicableReasonSummary("no_idjag")).toBe(
      "Cross App Access support not reported.",
    );
    expect(notApplicableReasonSummary(undefined)).toBe(
      "No Okta connection needed.",
    );
  });
});

describe("labels", () => {
  it("names readiness states", () => {
    expect(xaaStateLabel("needs_connection")).toBe("Not confirmed");
    expect(xaaStateLabel("connected")).toBe("Confirmed");
  });

  it("colors pending states as warnings and connected as success", () => {
    expect(xaaStateVariant("needs_agent")).toBe("warning");
    expect(xaaStateVariant("needs_connection")).toBe("warning");
    expect(xaaStateVariant("connected")).toBe("success");
    expect(xaaStateVariant("not_applicable")).toBe("neutral");
  });

  it("explains why a server is not applicable", () => {
    expect(notApplicableReasonLabel("no_idjag")).toMatch(/identity assertion/);
    expect(notApplicableReasonLabel(undefined)).toMatch(/No Okta connection/);
  });

  it("only explains client bindings that block confirmation", () => {
    expect(clientBindingNote("missing")).toMatch(/No app registration/);
    expect(clientBindingNote("ambiguous")).toMatch(/select one/);
    expect(clientBindingNote("bound")).toBeUndefined();
    expect(clientBindingNote("single")).toBeUndefined();
  });
});

describe("isConfirmable", () => {
  const base = {
    pending: true,
    state: "needs_connection",
    clientBinding: "bound",
  } as const;

  it("allows a pending row with a bound or single client", () => {
    expect(isConfirmable(base)).toBe(true);
    expect(isConfirmable({ ...base, clientBinding: "single" })).toBe(true);
  });

  it("blocks rows that need the agent first, are not applicable, or are connected", () => {
    expect(isConfirmable({ ...base, state: "needs_agent" })).toBe(false);
    expect(
      isConfirmable({ ...base, state: "not_applicable", pending: false }),
    ).toBe(false);
    expect(isConfirmable({ ...base, state: "connected", pending: false })).toBe(
      false,
    );
  });

  it("blocks rows that are no longer pending even in the needs_connection state", () => {
    expect(isConfirmable({ ...base, pending: false })).toBe(false);
  });

  it("blocks rows without one client to name", () => {
    expect(isConfirmable({ ...base, clientBinding: "missing" })).toBe(false);
    expect(isConfirmable({ ...base, clientBinding: "ambiguous" })).toBe(false);
  });
});

describe("normalizeAudience", () => {
  it("accepts an https issuer URL", () => {
    expect(normalizeAudience(" https://auth.example.com ")).toBe(
      "https://auth.example.com",
    );
    expect(normalizeAudience("https://auth.example.com/oauth")).toBe(
      "https://auth.example.com/oauth",
    );
  });

  it("enforces the API's 512-rune limit", () => {
    const host = "https://" + "a".repeat(AUDIENCE_MAX_RUNES - 8);
    expect(normalizeAudience(host)).toBe(host);
    expect(normalizeAudience(`${host}b`)).toBeUndefined();
    const multibyte = "https://" + "é".repeat(AUDIENCE_MAX_RUNES - 8);
    expect(normalizeAudience(multibyte)).toBe(multibyte);
    expect(normalizeAudience(`${multibyte}é`)).toBeUndefined();
  });

  it("rejects http, query, fragment, userinfo, and junk", () => {
    expect(normalizeAudience("http://auth.example.com")).toBeUndefined();
    expect(normalizeAudience("https://auth.example.com?x=1")).toBeUndefined();
    expect(normalizeAudience("https://auth.example.com#f")).toBeUndefined();
    expect(normalizeAudience("https://u:p@auth.example.com")).toBeUndefined();
    expect(normalizeAudience("auth.example.com")).toBeUndefined();
    expect(normalizeAudience("")).toBeUndefined();
  });
});

describe("buildConfirmRequests", () => {
  it("sends one audience per item and the app instance only when picked", () => {
    expect(
      buildConfirmRequests(
        [{ mcpServerId: "a" }, { mcpServerId: "b" }],
        "https://auth.example.com",
        undefined,
      ),
    ).toEqual([
      {
        connections: [
          { mcpServerId: "a", audience: "https://auth.example.com" },
          { mcpServerId: "b", audience: "https://auth.example.com" },
        ],
      },
    ]);
    expect(
      buildConfirmRequests([{ mcpServerId: "a" }], "https://x.test", "0oa1"),
    ).toEqual([
      {
        connections: [
          {
            mcpServerId: "a",
            audience: "https://x.test",
            oktaApplicationId: "0oa1",
          },
        ],
      },
    ]);
  });

  it("batches by the confirm cap", () => {
    const rows = Array.from({ length: XAA_CONFIRM_BATCH + 1 }, (_, i) => ({
      mcpServerId: `s${i}`,
    }));
    const requests = buildConfirmRequests(rows, "https://x.test", undefined);
    expect(requests).toHaveLength(2);
    expect(requests[0]?.connections).toHaveLength(XAA_CONFIRM_BATCH);
    expect(requests[1]?.connections[0]?.mcpServerId).toBe(
      `s${XAA_CONFIRM_BATCH}`,
    );
  });
});
