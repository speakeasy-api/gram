import { describe, expect, it } from "vitest";

import { auditViewQuery, parseAuditView } from "./audit-views";

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
