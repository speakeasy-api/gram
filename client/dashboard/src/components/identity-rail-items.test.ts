import { describe, expect, it } from "vitest";

import { identityRailItems } from "./identity-rail-items";
import type { useRoutes } from "@/routes";

/** Only the detail sub-routes the rail builds links from. */
function routes(): ReturnType<typeof useRoutes> {
  const page = (segment: string) => ({
    href: (urn: string) => `/identities/${urn}/${segment}`,
    active: false,
  });
  return {
    identities: {
      detail: {
        overview: page("overview"),
        access: page("access"),
        usage: page("usage"),
        security: page("security"),
        cost: page("cost"),
        connections: page("connections"),
        devices: page("devices"),
        activity: page("activity"),
      },
    },
  } as unknown as ReturnType<typeof useRoutes>;
}

const keys = (kind?: string) =>
  identityRailItems(routes(), "agent%3A1", "?window=7d", kind).map(
    (item) => item.key,
  );

describe("identityRailItems", () => {
  it("gives a person every sub-page", () => {
    expect(keys("user")).toEqual([
      "overview",
      "access",
      "usage",
      "security",
      "cost",
      "connections",
      "devices",
      "activity",
    ]);
  });

  it("drops the sub-pages an agent can never fill", () => {
    // Usage, cost and risk findings are keyed by a human subject. Each used to
    // render a panel whose only content was a sentence saying it had none.
    expect(keys("agent")).toEqual([
      "overview",
      "access",
      "devices",
      "connections",
      "activity",
    ]);
  });

  it("names an agent's tabs after what an agent holds", () => {
    const titles = Object.fromEntries(
      identityRailItems(routes(), "agent%3A1", "", "agent").map((item) => [
        item.key,
        item.title,
      ]),
    );
    expect(titles["access"]).toBe("Permissions");
    expect(titles["devices"]).toBe("Keys");
    expect(titles["connections"]).toBe("Sessions");
  });

  it("carries the current query string onto every link", () => {
    for (const item of identityRailItems(
      routes(),
      "agent%3A1",
      "?window=7d",
      "agent",
    )) {
      expect(item.href.endsWith("?window=7d")).toBe(true);
    }
  });
});
