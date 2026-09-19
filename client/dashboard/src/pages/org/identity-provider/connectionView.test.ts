import { describe, expect, it } from "vitest";

import {
  activeChecklistGroup,
  appInstanceOptions,
  AUDIENCE_MAX_RUNES,
  buildConfirmRequests,
  canConfirmReadiness,
  chunk,
  clientBindingNote,
  connectionStep,
  connectionStatusLabel,
  verificationReasonLabel,
  groupChecklist,
  humanizeOktaToken,
  isConfirmable,
  isConnectionChecked,
  isConnectionVerified,
  normalizeAudience,
  normalizeOktaOrgUrl,
  notApplicableReasonLabel,
  notApplicableReasonSummary,
  oktaAdminConsoleUrl,
  pluralize,
  XAA_CONFIRM_BATCH,
  xaaStateLabel,
  xaaStateVariant,
} from "./connectionView";

describe("connectionStep", () => {
  it("asks for the client id until it is submitted", () => {
    expect(
      connectionStep({ status: "pending", clientIdSubmitted: false }),
    ).toBe("submit_client_id");
  });

  it("asks to verify once the client id is in", () => {
    expect(connectionStep({ status: "pending", clientIdSubmitted: true })).toBe(
      "verify",
    );
  });

  it("treats verified and degraded as connected", () => {
    expect(
      connectionStep({ status: "verified", clientIdSubmitted: true }),
    ).toBe("connected");
    expect(
      connectionStep({ status: "degraded", clientIdSubmitted: true }),
    ).toBe("connected");
    expect(isConnectionChecked({ status: "degraded" })).toBe(true);
    expect(isConnectionChecked({ status: "pending" })).toBe(false);
  });

  it("gates the applications snapshot on a clean verification, like the server", () => {
    expect(isConnectionVerified({ status: "verified" })).toBe(true);
    expect(isConnectionVerified({ status: "degraded" })).toBe(false);
    expect(isConnectionVerified({ status: "pending" })).toBe(false);
  });

  it("accepts readiness confirmations on a degraded connection, like the server lock", () => {
    expect(canConfirmReadiness({ status: "verified" })).toBe(true);
    expect(canConfirmReadiness({ status: "degraded" })).toBe(true);
    expect(canConfirmReadiness({ status: "pending" })).toBe(false);
    expect(canConfirmReadiness({ status: "revoked" })).toBe(false);
  });
});

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

describe("groupChecklist", () => {
  const items = [
    {
      key: "a",
      group: "connect" as const,
      title: "",
      description: "",
      details: [],
      completed: true,
    },
    {
      key: "b",
      group: "connect" as const,
      title: "",
      description: "",
      details: [],
    },
    {
      key: "c",
      group: "cross_app_access" as const,
      title: "",
      description: "",
      details: [],
    },
  ];

  it("orders Connect before Cross App Access and counts only observed completions", () => {
    const groups = groupChecklist(items);
    expect(groups.map((g) => g.id)).toEqual(["connect", "cross_app_access"]);
    expect(groups[0]?.completedCount).toBe(1);
    expect(groups[1]?.completedCount).toBe(0);
  });

  it("explains the Okta registration separately from per-server access", () => {
    const group = groupChecklist(items).find(
      (g) => g.id === "cross_app_access",
    );
    expect(group?.description).toBe(
      "Register the Speakeasy AI agent once so Okta can issue Cross App Access assertions; per-server connections are handled on the Cross App Access tab.",
    );
  });

  it("drops empty groups", () => {
    expect(groupChecklist(items.slice(0, 2)).map((g) => g.id)).toEqual([
      "connect",
    ]);
  });

  it("expands Cross App Access after a completed verification", () => {
    expect(activeChecklistGroup({ status: "pending" })).toBe("connect");
    expect(activeChecklistGroup({ status: "degraded" })).toBe(
      "cross_app_access",
    );
    expect(activeChecklistGroup({ status: "verified" })).toBe(
      "cross_app_access",
    );
  });

  it.each(["okta.com", "oktapreview.com", "okta-emea.com", "okta.mil"])(
    "derives single- and multi-label console hosts for %s",
    (suffix) => {
      expect(oktaAdminConsoleUrl(`https://tenant.${suffix}`)).toBe(
        `https://tenant-admin.${suffix}`,
      );
      expect(oktaAdminConsoleUrl(`https://a.b.${suffix}`)).toBe(
        `https://a.b-admin.${suffix}`,
      );
      expect(oktaAdminConsoleUrl(`https://Tenant-Admin.${suffix}/`)).toBe(
        `https://tenant-admin.${suffix}`,
      );
    },
  );

  it("derives the admin console host", () => {
    expect(oktaAdminConsoleUrl("https://acme.okta.com")).toBe(
      "https://acme-admin.okta.com",
    );
    expect(oktaAdminConsoleUrl("https://acme.oktapreview.com")).toBe(
      "https://acme-admin.oktapreview.com",
    );
  });
});

describe("pluralize", () => {
  it("keeps the noun singular for one", () => {
    expect(pluralize(1, "selected server")).toBe("1 selected server");
    expect(pluralize(2, "server")).toBe("2 servers");
    expect(pluralize(0, "server")).toBe("0 servers");
  });

  it("puts revoked first regardless of the client id", () => {
    expect(
      connectionStep({ status: "revoked", clientIdSubmitted: false }),
    ).toBe("revoked");
  });
});

describe("normalizeOktaOrgUrl", () => {
  it("accepts Okta-owned hosts with at most one trailing slash", () => {
    expect(normalizeOktaOrgUrl("https://example.okta.com")).toBe(
      "https://example.okta.com",
    );
    expect(normalizeOktaOrgUrl(" https://Example.oktapreview.com/ ")).toBe(
      "https://example.oktapreview.com",
    );
    expect(normalizeOktaOrgUrl("https://sso.okta-emea.com")).toBe(
      "https://sso.okta-emea.com",
    );
  });

  it("rejects paths, queries, fragments, http, and foreign hosts", () => {
    expect(
      normalizeOktaOrgUrl("https://example.okta.com/admin"),
    ).toBeUndefined();
    expect(normalizeOktaOrgUrl("https://example.okta.com?x=1")).toBeUndefined();
    expect(normalizeOktaOrgUrl("https://example.okta.com#top")).toBeUndefined();
    expect(normalizeOktaOrgUrl("http://example.okta.com")).toBeUndefined();
    expect(normalizeOktaOrgUrl("https://login.example.com")).toBeUndefined();
    expect(normalizeOktaOrgUrl("https://okta.com.evil.dev")).toBeUndefined();
  });

  it("rejects the bare suffix, userinfo, ports, and non-plain hostnames", () => {
    expect(normalizeOktaOrgUrl("https://okta.com")).toBeUndefined();
    expect(normalizeOktaOrgUrl("https://okta.com/")).toBeUndefined();
    expect(
      normalizeOktaOrgUrl("https://user@example.okta.com"),
    ).toBeUndefined();
    expect(normalizeOktaOrgUrl("https://example.okta.com:443")).toBeUndefined();
    expect(normalizeOktaOrgUrl("https://ex_ample.okta.com")).toBeUndefined();
    expect(normalizeOktaOrgUrl("https://-example.okta.com")).toBeUndefined();
    expect(normalizeOktaOrgUrl("https://example-.okta.com")).toBeUndefined();
    expect(normalizeOktaOrgUrl("https://exämple.okta.com")).toBeUndefined();
    expect(normalizeOktaOrgUrl("https://ex..ample.okta.com")).toBeUndefined();
  });

  it("keeps hyphenated and multi-label subdomains", () => {
    expect(normalizeOktaOrgUrl("https://dev-1234.okta.com")).toBe(
      "https://dev-1234.okta.com",
    );
    expect(normalizeOktaOrgUrl("https://a.b.okta.mil/")).toBe(
      "https://a.b.okta.mil",
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

  it("humanizes Okta tokens with known spellings kept", () => {
    expect(humanizeOktaToken("OPENID_CONNECT")).toBe("OpenID Connect");
    expect(humanizeOktaToken("SAML_2_0")).toBe("SAML 2.0");
    expect(humanizeOktaToken("ACTIVE")).toBe("Active");
    expect(humanizeOktaToken("SOME_NEW_MODE")).toBe("Some new mode");
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

describe("chunk", () => {
  it("splits into fixed batches and keeps order", () => {
    expect(chunk([1, 2, 3, 4, 5], 2)).toEqual([[1, 2], [3, 4], [5]]);
    expect(chunk([], 2)).toEqual([]);
  });
});

describe("connection setup copy", () => {
  it("distinguishes incomplete setup, verified access, and issues", () => {
    expect(connectionStatusLabel("pending")).toBe("Setup incomplete");
    expect(connectionStatusLabel("verified")).toBe("Verified");
    expect(connectionStatusLabel("degraded")).toBe("Needs attention");
  });

  it("explains technical checks without claiming to inspect exact roles", () => {
    expect(verificationReasonLabel("missing_scope")).toContain(
      "permissions (scopes)",
    );
    expect(verificationReasonLabel("dpop_not_bound")).toContain(
      "token protection (DPoP)",
    );
    expect(verificationReasonLabel("key_not_fetched")).toContain(
      "public key URL (JWKS)",
    );
    expect(verificationReasonLabel("missing_role")).toBe(
      "Check the app’s admin role in Okta. Speakeasy could not confirm the required access.",
    );
  });
});
