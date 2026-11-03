import { MemoryRouter } from "react-router";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { GramCore } from "@gram/client/core.js";
import { HTTPClient } from "@gram/client/lib/http.js";
import { GramProvider } from "@gram/client/react-query/_context.js";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";

import { McpServersTab } from "./McpServersTab";
import type { UpstreamAuthEnforcementStatus } from "@/hooks/useUpstreamAuthEnforcement";

const enforcement = vi.hoisted(() => ({
  status: "legacy" as UpstreamAuthEnforcementStatus,
}));
vi.mock("@/hooks/useUpstreamAuthEnforcement", () => ({
  useUpstreamAuthEnforcement: () => enforcement.status,
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope: () => true }),
}));
vi.mock("@/contexts/Sdk", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  useSlugs: () => ({ orgSlug: "org", projectSlug: "proj" }),
}));

const queryClients: QueryClient[] = [];
afterEach(() => {
  cleanup();
  queryClients.forEach((client) => client.clear());
  queryClients.length = 0;
  enforcement.status = "legacy";
});

function setup() {
  const requests: Request[] = [];
  const client = new GramCore({
    serverURL: "https://api.example.test",
    retryConfig: { strategy: "none" },
    httpClient: new HTTPClient({
      fetcher: async (request) => {
        const req = request as Request;
        requests.push(req);
        return new Response(
          JSON.stringify({
            items: [
              {
                id: "11111111-1111-4111-8111-111111111111",
                project_id: "22222222-2222-4222-8222-222222222222",
                project_slug: "proj",
                name: "Example server",
                slug: "example-server",
                url: "https://upstream.example.test/mcp",
              },
            ],
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      },
    }),
  });
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  queryClients.push(queryClient);
  const view = render(<McpServersTab clientId="client-a" />, {
    wrapper: ({ children }) => (
      <QueryClientProvider client={queryClient}>
        <GramProvider client={client}>
          <MemoryRouter>{children}</MemoryRouter>
        </GramProvider>
      </QueryClientProvider>
    ),
  });
  return { ...view, requests, user: userEvent.setup() };
}

async function openRowMenu(user: ReturnType<typeof userEvent.setup>) {
  await screen.findByText("Example server");
  const trigger = screen
    .getAllByRole("button")
    .find((button) => button.querySelector("svg"));
  if (trigger) await user.click(trigger);
}

it("offers the legacy removal when the organization is not enforced", async () => {
  enforcement.status = "legacy";
  const { user } = setup();
  await openRowMenu(user);
  expect(await screen.findByText("Remove from server")).toBeTruthy();
  expect(screen.queryByText("Manage in Identity")).toBeNull();
});

it("sends an enforced organization to the server's own Identity", async () => {
  enforcement.status = "enforced";
  const { user } = setup();
  await openRowMenu(user);
  expect(await screen.findByText("Manage in Identity")).toBeTruthy();
  expect(
    screen.queryByText("Remove from server"),
    "an enforced organization never sees the legacy binding control",
  ).toBeNull();
});

it.each(["loading", "error"] as const)(
  "withholds the legacy removal while the state is %s",
  async (status) => {
    enforcement.status = status;
    const { user } = setup();
    await openRowMenu(user);
    const removal = screen.queryByText("Remove from server");
    if (removal) {
      expect(removal.closest("[aria-disabled], button")).toBeTruthy();
    }
    expect(screen.queryByText("Manage in Identity")).toBeNull();
  },
);
