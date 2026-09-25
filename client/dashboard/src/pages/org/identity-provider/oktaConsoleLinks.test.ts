import { describe, expect, it } from "vitest";
import {
  normalizeOktaOrgUrl,
  oktaAdminConsoleUrl,
  oktaApplicationsUrl,
  oktaConnectionsUrl,
  oktaConsoleUrl,
} from "./oktaConsoleLinks";

const connections =
  "https://example-admin.okta.com/admin/workload-principals/ai-agents/example-agent/resource-connections";

describe("Okta connection list links", () => {
  it.each(["/create", "/create/"])(
    "opens the existing connections before creating one: %s",
    (suffix) => {
      expect(oktaConnectionsUrl(connections + suffix)).toBe(connections);
    },
  );
  it("retains an existing connection-list URL", () => {
    expect(oktaConnectionsUrl(connections)).toBe(connections);
  });
  it.each([
    undefined,
    "not a URL",
    "javascript:alert(1)",
    "http://example.okta.com",
  ])("rejects unusable links: %s", (link) => {
    expect(oktaConnectionsUrl(link)).toBeUndefined();
  });
});

describe("Okta Applications links", () => {
  it.each(["okta.com", "oktapreview.com", "okta-emea.com", "okta.mil"])(
    "opens the admin Applications list for %s",
    (suffix) => {
      expect(oktaApplicationsUrl(`https://tenant.${suffix}`)).toBe(
        `https://tenant-admin.${suffix}/admin/apps/active`,
      );
      expect(oktaApplicationsUrl(`https://tenant-admin.${suffix}/`)).toBe(
        `https://tenant-admin.${suffix}/admin/apps/active`,
      );
    },
  );
  it.each([
    undefined,
    "not a URL",
    "javascript:alert(1)",
    "https://unrelated.example.com",
  ])("omits invalid tenant URLs: %s", (url) => {
    expect(oktaApplicationsUrl(url)).toBeUndefined();
  });
});

describe("Okta console URL validation", () => {
  it.each(["okta.com", "oktapreview.com", "okta-emea.com", "okta.mil"])(
    "allows create links on %s",
    (suffix) => {
      const url = `https://tenant-admin.${suffix}/resource-connections/create`;
      expect(oktaConsoleUrl(url)).toBe(url);
      expect(oktaConnectionsUrl(url)).toBe(
        `https://tenant-admin.${suffix}/resource-connections`,
      );
    },
  );
  it.each([
    "javascript:alert(1)",
    "data:text/html,unsafe",
    "blob:https://tenant.okta.com/example",
    "http://tenant.okta.com/create",
    "https://attacker.example/create",
    "https://tenant.okta.com.attacker.example/create",
    "https://notokta.com/create",
    "https://okta.com/create",
    "https://tenant.okta.com@attacker.example/create",
    "https://user:password@tenant.okta.com/create",
    "https://tenant.okta.com:8443/create",
  ])("rejects unsafe console links: %s", (url) => {
    expect(oktaConsoleUrl(url)).toBeUndefined();
    expect(oktaConnectionsUrl(url)).toBeUndefined();
  });
});

describe("oktaAdminConsoleUrl", () => {
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

  it("maps the admin console address to the org", () => {
    expect(normalizeOktaOrgUrl("https://example-admin.okta.com/")).toBe(
      "https://example.okta.com",
    );
    expect(normalizeOktaOrgUrl("https://-admin.okta.com")).toBeUndefined();
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
