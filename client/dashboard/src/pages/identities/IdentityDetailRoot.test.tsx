import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import IdentityDetailRoot from "./IdentityDetailRoot";

const mocks = vi.hoisted(() => ({ orgRead: false, loading: false }));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org_example" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => mocks.orgRead, isLoading: mocks.loading }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({ agents: { href: () => "/agents" } }),
}));
// Do not mount the panels: these regressions exercise the route's authorization boundary.
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ scope }: { scope: string[] }) => (
    <div>Scope: {scope.join(",")}</div>
  ),
}));
function Destination() {
  const { pathname, search } = useLocation();
  return (
    <div>
      {pathname}
      {search}
    </div>
  );
}
function setup(urn: string) {
  render(
    <MemoryRouter initialEntries={[`/identities/${encodeURIComponent(urn)}`]}>
      <Routes>
        <Route
          path="/identities/:identityUrn"
          element={<IdentityDetailRoot />}
        />
        <Route path="/agents" element={<Destination />} />
      </Routes>
    </MemoryRouter>,
  );
}
afterEach(cleanup);
beforeEach(() => {
  mocks.orgRead = false;
  mocks.loading = false;
});
it("opens agent management for a project reader lacking organization read", () => {
  setup("agent:agent_example");
  expect(screen.getByText("/agents?id=agent_example")).toBeTruthy();
});
it("keeps human profiles behind organization read", () => {
  setup("user:user_example");
  expect(screen.getByText("Scope: org:read")).toBeTruthy();
});
it("keeps the agent identity profile for organization readers", () => {
  mocks.orgRead = true;
  setup("agent:agent_example");
  expect(screen.getByText("Scope: org:read")).toBeTruthy();
});
it("waits for grants before choosing the agent destination", () => {
  mocks.loading = true;
  setup("agent:agent_example");
  expect(screen.queryByText("/agents?id=agent_example")).toBeNull();
});
