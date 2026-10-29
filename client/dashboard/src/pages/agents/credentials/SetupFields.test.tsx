import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import { ComputedStatus } from "./SetupFields";
import type { SetupValues } from "./workloadTokenEndpoints";

vi.mock("@/routes", () => ({
  useRoutes: ({ projectSlug }: { projectSlug: string }) => ({
    mcp: {
      details: {
        settings: {
          href: (param: string) =>
            `/projects/${projectSlug}/mcp/${param}/settings`,
        },
      },
    },
  }),
}));
const switchMocks = vi.hoisted(() => ({
  canWrite: true,
  clients: [] as { id: string }[],
  link: vi.fn(async () => ({})),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    remoteSessionClients: {
      list: async () =>
        (async function* () {
          yield { result: { items: switchMocks.clients } };
        })(),
    },
  }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => switchMocks.canWrite }),
}));
vi.mock("@/lib/linkUserSessionIssuer", () => ({
  linkMcpServerUserSessionIssuer: switchMocks.link,
}));
vi.mock("@gram/client/react-query/userSessionIssuerAttachImpact.js", () => ({
  useUserSessionIssuerAttachImpact: () => ({
    data: {
      candidateIssuer: { issuerUrl: "https://gram.example.com/oauth/usi/org" },
    },
    isError: false,
    isFetching: false,
  }),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
afterEach(() => {
  cleanup();
  switchMocks.canWrite = true;
  switchMocks.clients = [];
  switchMocks.link.mockClear();
});

function withProviders(children: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return (
    <QueryClientProvider client={client}>
      <MemoryRouter>{children}</MemoryRouter>
    </QueryClientProvider>
  );
}

const github = {
  id: "gh",
  name: "GitHub",
  mcpUrl: "https://gram.example.com/mcp/github",
  issuerId: "private",
  projectSlug: "default",
  slug: "github",
};
const linear = {
  id: "ln",
  name: "Linear",
  mcpUrl: "https://gram.example.com/mcp/linear",
  issuerId: "other",
  projectSlug: "default",
  slug: "linear",
};

function setupValues(overrides: Partial<SetupValues>): SetupValues {
  return {
    values: { token_endpoint: "https://gram.example.com/oauth/usi/org/token" },
    unavailableReason: null,
    endpointGroups: [
      { label: "Organization", options: [{ id: "org", label: "org" }] },
    ],
    selectedEndpointId: "org",
    onEndpointChange: vi.fn<(id: string) => void>(),
    refreshAndSelect: vi.fn<(id: string) => Promise<void>>(),
    coveredServers: [],
    uncoveredServers: [],
    ...overrides,
  };
}

it("names each server the selected issuer's token won't work for, with its fix", () => {
  const onEndpointChange = vi.fn<(id: string) => void>();
  render(
    <MemoryRouter>
      <ComputedStatus
        setupValues={setupValues({
          onEndpointChange,
          uncoveredServers: [
            { server: linear, reason: "another_issuer" },
            { server: github, reason: "not_shared" },
          ],
        })}
      />
    </MemoryRouter>,
  );

  expect(
    screen.getByText("This token won't work for 2 of this agent's servers"),
  ).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Use its issuer" }));
  expect(onEndpointChange).toHaveBeenCalledWith("other");
  expect(
    screen
      .getByRole("link", { name: "Open sign-in settings" })
      .getAttribute("href"),
  ).toBe("/projects/default/mcp/github/settings#authentication");
});

it("lists the servers behind an unavailable token endpoint", () => {
  render(
    <MemoryRouter>
      <ComputedStatus
        setupValues={setupValues({
          values: undefined,
          unavailableReason: "No shared issuer.",
          endpointGroups: [],
          selectedEndpointId: "",
          uncoveredServers: [{ server: github, reason: "not_shared" }],
        })}
      />
    </MemoryRouter>,
  );

  expect(screen.getByText("No shared issuer.")).toBeTruthy();
  expect(screen.getByText("GitHub")).toBeTruthy();
  expect(
    screen.getByRole("link", { name: "Open sign-in settings" }),
  ).toBeTruthy();
});

it("stays quiet when the selected issuer's token works for every server", () => {
  render(
    <MemoryRouter>
      <ComputedStatus setupValues={setupValues({ coveredServers: [github] })} />
    </MemoryRouter>,
  );
  expect(screen.queryByText(/This token won't work/)).toBeNull();
});

const notShared = {
  server: { ...github, projectId: "proj", resourceId: "gh" },
  reason: "not_shared" as const,
  switchTo: { id: "org", label: "acme-workforce" },
};

it("switches a server on a shared credential to the shared issuer after confirming", async () => {
  render(
    withProviders(
      <ComputedStatus
        setupValues={setupValues({ uncoveredServers: [notShared] })}
      />,
    ),
  );
  await waitFor(() =>
    expect(
      screen
        .getByRole("button", { name: "Switch to acme-workforce" })
        .hasAttribute("disabled"),
    ).toBe(false),
  );
  fireEvent.click(
    screen.getByRole("button", { name: "Switch to acme-workforce" }),
  );
  expect(
    await screen.findByText("Switch GitHub to acme-workforce?"),
  ).toBeTruthy();
  expect(screen.getByText(/will need to sign in again/)).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Switch sign-in" }));
  await waitFor(() =>
    expect(switchMocks.link).toHaveBeenCalledWith(
      expect.anything(),
      "gh",
      "org",
      undefined,
      "default",
    ),
  );
});

it("sends a server people connect their own accounts to to its settings instead", async () => {
  switchMocks.clients = [{ id: "client" }];
  render(
    withProviders(
      <ComputedStatus
        setupValues={setupValues({ uncoveredServers: [notShared] })}
      />,
    ),
  );
  expect(
    await screen.findByText(/People connect their own accounts/),
  ).toBeTruthy();
  expect(
    screen.getByRole("link", { name: "Open sign-in settings" }),
  ).toBeTruthy();
  expect(
    screen.queryByRole("button", { name: "Switch to acme-workforce" }),
  ).toBeNull();
});

it("asks someone who can edit the server when the reader cannot", () => {
  switchMocks.canWrite = false;
  render(
    withProviders(
      <ComputedStatus
        setupValues={setupValues({ uncoveredServers: [notShared] })}
      />,
    ),
  );
  expect(
    screen.getByText(
      /Ask someone who can edit GitHub to switch it to acme-workforce/,
    ),
  ).toBeTruthy();
  expect(
    screen.queryByRole("button", { name: "Switch to acme-workforce" }),
  ).toBeNull();
});
