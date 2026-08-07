import { describe, expect, it } from "vitest";
import { blockerSummary } from "./blockerSummary";

describe("blockerSummary", () => {
  it("reports no blockers when nothing references the issuer", () => {
    expect(blockerSummary(0, 0)).toBe(
      "No clients are registered with this provider.",
    );
  });

  // The distinction the acceptance criteria turn on: a platform admin must be
  // able to tell which blockers they can clear from the ones they cannot even
  // see, since tenant clients never appear in any platform listing.
  it("names platform clients as the admin's to delete", () => {
    const summary = blockerSummary(2, 0);
    expect(summary).toContain("2 platform clients");
    expect(summary).toContain("delete them here first");
    expect(summary).not.toContain("tenant-owned");
  });

  it("names tenant clients as the owning organization's to remove", () => {
    const summary = blockerSummary(0, 1);
    expect(summary).toContain("1 tenant-owned client");
    expect(summary).toContain("only the owning organization can remove it");
    expect(summary).not.toContain("platform client");
  });

  it("reports both populations separately when both block", () => {
    const summary = blockerSummary(1, 3);
    expect(summary).toContain("1 platform client");
    expect(summary).toContain("3 tenant-owned clients");
    expect(summary).toContain(" and ");
  });
});
