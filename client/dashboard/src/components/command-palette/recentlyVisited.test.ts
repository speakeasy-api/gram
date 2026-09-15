import { beforeEach, describe, expect, it } from "vitest";
import {
  pageLabel,
  recordVisit,
  removeVisitsMatching,
  shouldRemoveRestrictedRecents,
} from "./recentlyVisited";

const BASE = "/acme/projects/default";

beforeEach(() => localStorage.clear());

describe("pageLabel", () => {
  it("uses the route title for the section itself", () => {
    expect(pageLabel("Project Assistant", `${BASE}/chat`, `${BASE}/chat`)).toBe(
      "Project Assistant",
    );
  });

  it("uses a human slug on detail pages", () => {
    expect(
      pageLabel(
        "Sources",
        `${BASE}/sources`,
        `${BASE}/sources/externalmcp/notion`,
      ),
    ).toBe("notion");
  });

  it("does not show a UUID as the label", () => {
    expect(
      pageLabel(
        "Project Assistant",
        `${BASE}/chat`,
        `${BASE}/chat/b4b1d55f-1234-5678-9abc-def012345678`,
      ),
    ).toBe("Project Assistant");
  });

  it("waits for authoritative access before cleaning restricted recents", () => {
    expect(
      shouldRemoveRestrictedRecents({ canAccess: false, isLoading: true }),
    ).toBe(false);
    expect(
      shouldRemoveRestrictedRecents({ canAccess: false, isLoading: false }),
    ).toBe(true);
    expect(
      shouldRemoveRestrictedRecents({ canAccess: true, isLoading: false }),
    ).toBe(false);
  });

  it("removes denied routes from stored recents", () => {
    recordVisit("user", "acme", undefined, {
      label: "Killswitch",
      href: "/acme/killswitch/ks-1",
    });
    recordVisit("user", "acme", undefined, {
      label: "Team",
      href: "/acme/team",
    });
    removeVisitsMatching("user", "acme", undefined, (entry) =>
      entry.href.startsWith("/acme/killswitch"),
    );
    const stored = Array.from({ length: localStorage.length }, (_, index) =>
      localStorage.getItem(localStorage.key(index)!),
    ).join("");
    expect(stored).not.toContain("killswitch");
    expect(stored).toContain("/acme/team");
  });

  it("does not show other opaque ids as the label", () => {
    expect(
      pageLabel(
        "Assistants",
        `${BASE}/assistants`,
        `${BASE}/assistants/${"a".repeat(32)}`,
      ),
    ).toBe("Assistants");
  });
});
