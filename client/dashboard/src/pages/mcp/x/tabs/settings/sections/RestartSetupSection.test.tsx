import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { McpServer } from "@gram/client/models/components/mcpserver";
import { providerProgressKey } from "@/pages/mcp/x/setup/provider-setup";
import { RestartSetupSection } from "./RestartSetupSection";

const mocks = vi.hoisted(() => ({
  markCompleted: vi.fn(),
  invalidate: vi.fn(),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    mcpServers: { setSetupGuideCompleted: mocks.markCompleted },
  }),
}));
vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
vi.mock("@/lib/mcp-server-visibility", () => ({
  invalidateMcpServerQueries: mocks.invalidate,
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: { details: { setup: { href: (id: string) => `/mcp/${id}/setup` } } },
  }),
}));

const server: McpServer = {
  id: "server",
  projectId: "project",
  remoteMcpServerId: "remote",
  name: "Slack",
  slug: "slack",
  visibility: "private",
  networkAccessMode: "public_only",
  setupGuideCompletedAt: new Date(),
  createdAt: new Date(),
  updatedAt: new Date(),
};

function Location() {
  return <output aria-label="Location">{useLocation().pathname}</output>;
}
function renderSection(mcpServer: McpServer = server) {
  return render(
    <MemoryRouter initialEntries={["/settings"]}>
      <Routes>
        <Route
          path="*"
          element={<RestartSetupSection mcpServer={mcpServer} />}
        />
      </Routes>
      <Location />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  vi.resetAllMocks();
  mocks.markCompleted.mockResolvedValue(server);
  mocks.invalidate.mockResolvedValue(undefined);
});
afterEach(() => {
  cleanup();
  sessionStorage.clear();
});

const savedHere = providerProgressKey(
  "slack",
  "user",
  "org",
  "project",
  "server",
);
const savedElsewhere = providerProgressKey(
  "slack",
  "user",
  "org",
  "project",
  "other-server",
);

it("clears the done mark and opens Setup", async () => {
  renderSection();
  await userEvent.click(screen.getByRole("button", { name: "Restart setup" }));
  await waitFor(() =>
    expect(screen.getByLabelText("Location").textContent).toBe(
      "/mcp/slack/setup",
    ),
  );
  expect(mocks.markCompleted).toHaveBeenCalledWith({
    setSetupGuideCompletedRequestBody: { id: "server", completed: false },
  });
  expect(mocks.invalidate).toHaveBeenCalledTimes(1);
});

it("forgets this server's saved Setup step so Setup starts from the beginning", async () => {
  sessionStorage.setItem(savedHere, JSON.stringify({ step: "test" }));
  sessionStorage.setItem(savedElsewhere, JSON.stringify({ step: "test" }));
  renderSection();
  await userEvent.click(screen.getByRole("button", { name: "Restart setup" }));
  await waitFor(() =>
    expect(screen.getByLabelText("Location").textContent).toBe(
      "/mcp/slack/setup",
    ),
  );
  expect(sessionStorage.getItem(savedHere)).toBeNull();
  expect(sessionStorage.getItem(savedElsewhere)).not.toBeNull();
});

it("stays in Settings with an error when the restart can't be saved", async () => {
  mocks.markCompleted.mockRejectedValue(new Error("unavailable"));
  sessionStorage.setItem(savedHere, JSON.stringify({ step: "test" }));
  renderSection();
  await userEvent.click(screen.getByRole("button", { name: "Restart setup" }));
  expect(
    await screen.findByText("Unable to restart setup. Try again."),
  ).toBeDefined();
  expect(screen.getByLabelText("Location").textContent).toBe("/settings");
  expect(sessionStorage.getItem(savedHere)).not.toBeNull();
  expect(
    screen.getByRole<HTMLButtonElement>("button", { name: "Restart setup" })
      .disabled,
  ).toBe(false);
});
