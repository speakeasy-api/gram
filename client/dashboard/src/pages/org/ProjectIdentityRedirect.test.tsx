import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import ProjectIdentityRedirect from "./ProjectIdentityRedirect";

vi.mock("@/contexts/Auth", () => ({ useProject: () => ({ slug: "working" }) }));
afterEach(cleanup);
function Destination() {
  const location = useLocation();
  return (
    <div>
      {location.pathname}
      {location.search}
      {location.hash}
    </div>
  );
}
it.each([
  [
    "/org/agent-management?id=agent-1",
    "/org/projects/working/agent-management?id=agent-1",
  ],
  [
    "/org/mcp-sessions?project=selected&subjectUrn=user%3A1",
    "/org/projects/selected/mcp-sessions?subjectUrn=user%3A1",
  ],
  [
    "/org/remote-identity-providers/provider/clients/client/sessions#details",
    "/org/projects/working/remote-identity-providers/provider/clients/client/sessions#details",
  ],
])("redirects legacy URL %s", (source, destination) => {
  render(
    <MemoryRouter initialEntries={[source]}>
      <Routes>
        <Route
          path="/:orgSlug/projects/:projectSlug/*"
          element={<Destination />}
        />
        <Route path="/:orgSlug/*" element={<ProjectIdentityRedirect />} />
      </Routes>
    </MemoryRouter>,
  );
  expect(screen.getByText(destination)).toBeTruthy();
});
