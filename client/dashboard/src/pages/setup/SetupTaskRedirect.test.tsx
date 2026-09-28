import { cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import SetupTaskRedirect from "./SetupTaskRedirect";

const mocks = vi.hoisted(() => ({
  params: { taskSlug: "" },
  navigate: vi.fn(),
}));

vi.mock("react-router", () => ({
  useParams: () => mocks.params,
  Navigate: ({ to }: { to: string }) => {
    mocks.navigate(to);
    return null;
  },
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ setup: { href: () => "/org/setup" } }),
}));

afterEach(() => {
  cleanup();
  mocks.navigate.mockReset();
});

describe("SetupTaskRedirect", () => {
  it.each([
    ["idp", "/org/setup?task=idp"],
    ["identity-provider", "/org/setup?task=idp"],
    ["wizard", "/org/setup"],
    ["nope", "/org/setup"],
  ])("sends setup/%s to %s", (slug, target) => {
    mocks.params.taskSlug = slug;
    render(<SetupTaskRedirect />);
    expect(mocks.navigate).toHaveBeenCalledWith(target);
  });
});
