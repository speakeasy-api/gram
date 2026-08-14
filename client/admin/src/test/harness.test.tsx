import { useQuery } from "@tanstack/react-query";
import { createRootRoute, createRoute } from "@tanstack/react-router";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { useState } from "react";
import { afterEach, describe, expect, it } from "vitest";

import { Badge } from "@/components/ui/badge";
import { badgeTone } from "@/lib/badgeTone";
import { renderRouteTree, renderWithApp } from "./harness";

afterEach(cleanup);

function Probe() {
  const { data } = useQuery({
    queryKey: ["probe"],
    queryFn: () => Promise.resolve("query ran"),
  });
  const [clicks, setClicks] = useState(0);

  return (
    <div>
      <Badge variant="outline" className={badgeTone.warning}>
        trialing
      </Badge>
      <p>{data}</p>
      <button type="button" onClick={() => setClicks(clicks + 1)}>
        clicked {clicks}
      </button>
    </div>
  );
}

function SearchProbe() {
  const { q } = organizationsRoute.useSearch();
  return <p>q is {q}</p>;
}

const rootRoute = createRootRoute();
const organizationsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/organizations",
  validateSearch: (search: Record<string, unknown>): { q: string } => ({
    q: typeof search["q"] === "string" ? search["q"] : "",
  }),
  component: SearchProbe,
});

describe("harness", () => {
  it("mounts a component with a query client and a router at the given path", async () => {
    const { router } = await renderWithApp(<Probe />, {
      initialPath: "/organizations",
    });

    // getBy is safe: renderRouteTree awaits router.load() before it paints.
    const badge = screen.getByText("trialing");
    expect(badge.className.split(" ")).toEqual(
      expect.arrayContaining(badgeTone.warning.split(" ")),
    );
    expect(await screen.findByText("query ran")).toBeTruthy();
    expect(router.state.location.pathname).toBe("/organizations");

    fireEvent.click(screen.getByRole("button", { name: "clicked 0" }));
    expect(screen.getByRole("button", { name: "clicked 1" })).toBeTruthy();
  });

  it("mounts a route tree, so validateSearch and the route hooks run", async () => {
    await renderRouteTree(rootRoute.addChildren([organizationsRoute]), {
      initialPath: "/organizations?q=acme",
    });

    expect(screen.getByText("q is acme")).toBeTruthy();
  });
});
