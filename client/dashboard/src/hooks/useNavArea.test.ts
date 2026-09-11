import { renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

const pathname = vi.hoisted(() => ({ current: "/" }));
vi.mock("react-router", () => ({
  useLocation: () => ({ pathname: pathname.current }),
}));

import { useNavArea } from "./useNavArea";

function areaAt(path: string) {
  pathname.current = path;
  return renderHook(() => useNavArea()).result.current;
}

describe("useNavArea", () => {
  // The detail page's own segment sits below the page slug, so the group the
  // reader came from has to stay open while they are on it.
  it.each([
    "/org/projects/proj/identities",
    "/org/projects/proj/identities/user%3A1/overview",
  ])("puts %s in Observability", (path) => {
    expect(areaAt(path)).toBe("Observability");
  });

  it("leaves ungrouped project pages without an area", () => {
    expect(areaAt("/org/projects/proj")).toBeUndefined();
  });
});
