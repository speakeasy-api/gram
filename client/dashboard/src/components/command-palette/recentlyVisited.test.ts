import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  getRecentLabelOverride,
  useRecentLabelOverride,
  removeResourceVisits,
  pageLabel,
  recordVisit,
  removeVisitsMatching,
  shouldRemoveRestrictedRecents,
} from "./recentlyVisited";

const BASE = "/acme/projects/default";

beforeEach(() => localStorage.clear());
afterEach(cleanup);

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

it("forgets resource overrides and scoped visits without matching sibling resources", () => {
  const resource = `${BASE}/identities/agent%3Aprivate`;
  const overview = `${resource}/overview`;
  const permissions = `${resource}/permissions`;
  const other = `${resource}-other/overview`;
  renderHook(() => {
    useRecentLabelOverride(overview, "Private name");
    useRecentLabelOverride(permissions, "Private name");
    useRecentLabelOverride(other, "Other name");
  });
  for (const href of [resource, overview, permissions, other]) {
    recordVisit("user", "acme", "default", {
      href,
      label: href === other ? "Other name" : "Private name",
    });
  }
  recordVisit("other-user", "acme", "default", {
    href: overview,
    label: "Other user visit",
  });
  recordVisit("user", "acme", "other-project", {
    href: overview,
    label: "Other project visit",
  });
  recordVisit("user", "other-org", "default", {
    href: overview,
    label: "Other org visit",
  });
  removeResourceVisits("user", "acme", "default", resource);
  expect(getRecentLabelOverride(overview)).toBeUndefined();
  expect(getRecentLabelOverride(permissions)).toBeUndefined();
  expect(getRecentLabelOverride(other)).toBe("Other name");
  const stored = Array.from({ length: localStorage.length }, (_, i) =>
    localStorage.getItem(localStorage.key(i)!),
  ).join("");
  expect(stored).not.toContain("Private name");
  expect(stored).toContain("Other name");
  expect(stored).toContain("Other user visit");
  expect(stored).toContain("Other project visit");
  expect(stored).toContain("Other org visit");
  // A subsequent App recorder pass cannot restore the private label.
  recordVisit("user", "acme", "default", {
    href: overview,
    label: getRecentLabelOverride(overview) ?? "Agents",
  });
  const rerecorded = Array.from({ length: localStorage.length }, (_, i) =>
    localStorage.getItem(localStorage.key(i)!),
  ).join("");
  expect(rerecorded).not.toContain("Private name");
  expect(rerecorded).toContain("Agents");
});
