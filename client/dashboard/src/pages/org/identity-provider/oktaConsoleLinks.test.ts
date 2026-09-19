import { describe, expect, it } from "vitest";
import {
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
