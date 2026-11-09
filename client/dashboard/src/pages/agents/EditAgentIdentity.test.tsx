import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { afterEach, expect, it, vi } from "vitest";

import EditAgentIdentity from "./EditAgentIdentity";

vi.mock("@/contexts/Sdk", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  useSlugs: () => ({ orgSlug: "acme", projectSlug: "default" }),
}));
// Only the redirect is under test; the sign-in flow it keeps has its own.
vi.mock("./credentials/CredentialsWizard", () => ({
  CredentialsWizard: () => <div>Agent sign-in flow</div>,
}));

function Landed(): JSX.Element {
  const location = useLocation();
  return <p>{`${location.pathname}${location.search}`}</p>;
}

function open(search: string) {
  render(
    <MemoryRouter
      initialEntries={[
        `/acme/projects/default/identities/agents/agent_1/edit${search}`,
      ]}
    >
      <Routes>
        <Route
          path="/acme/projects/default/identities/agents/:agentId/edit"
          element={<EditAgentIdentity />}
        />
        <Route path="*" element={<Landed />} />
      </Routes>
    </MemoryRouter>,
  );
}

afterEach(cleanup);

// Edit used to walk the whole setup wizard from the name. The name is now
// renamed in the agent page's header and the servers changed on its MCP
// sign-in tab, so old links and bookmarks land there.
it.each([
  ["", ""],
  ["?step=0", ""],
  ["?step=2", ""],
  ["?step=1", "?view=servers"],
])("sends %s to the MCP sign-in tab", (search, view) => {
  open(search);
  expect(
    screen.getByText(
      `/acme/projects/default/identities/agent%3Aagent_1/mcp-sign-in${view}`,
    ),
  ).toBeTruthy();
});
