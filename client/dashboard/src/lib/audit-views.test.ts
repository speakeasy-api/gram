import { describe, expect, it } from "vitest";

import {
  auditViewQuery,
  parseAuditView,
  parseResourceFilter,
  resourceFilterValue,
} from "./audit-views";

describe("parseAuditView", () => {
  it("defaults to changes, including older configuration links", () => {
    expect(parseAuditView("changes")).toBe("changes");
    expect(parseAuditView("configuration")).toBe("changes");
    expect(parseAuditView("")).toBe("changes");
    expect(parseAuditView("bogus")).toBe("changes");
  });

  it("keeps the other views", () => {
    expect(parseAuditView("system")).toBe("system");
    expect(parseAuditView("security")).toBe("security");
    expect(parseAuditView("all")).toBe("all");
  });
});

describe("auditViewQuery", () => {
  it("maps each view to its category and origin", () => {
    expect(auditViewQuery("changes")).toEqual({
      category: "configuration",
      origin: "user",
    });
    expect(auditViewQuery("system")).toEqual({
      category: "configuration",
      origin: "system",
    });
    expect(auditViewQuery("security")).toEqual({ category: "security" });
    expect(auditViewQuery("all")).toEqual({ category: "configuration" });
  });
});

describe("resource filter", () => {
  it("round-trips a subject, keeping colons in the id", () => {
    const value = resourceFilterValue({
      subjectType: "principal",
      subjectId: "user:abc",
    });
    expect(value).toBe("principal:user:abc");
    expect(parseResourceFilter(value)).toEqual({
      subjectType: "principal",
      subjectId: "user:abc",
    });
  });

  it("rejects empty or malformed values", () => {
    expect(parseResourceFilter(null)).toBeNull();
    expect(parseResourceFilter("")).toBeNull();
    expect(parseResourceFilter(":id")).toBeNull();
    expect(parseResourceFilter("toolset:")).toBeNull();
    expect(parseResourceFilter("toolset")).toBeNull();
  });
});
