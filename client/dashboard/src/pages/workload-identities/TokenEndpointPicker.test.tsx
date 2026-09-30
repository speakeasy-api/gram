import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { WorkloadConnectionEndpoint } from "@gram/client/models/components/workloadconnectionendpoint.js";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { TokenEndpointPicker } from "./TokenEndpointPicker";

const PROJECT_ID = "44444444-4444-4444-8444-444444444444";
const SERVER_ID = "55555555-5555-4555-8555-555555555555";
const OTHER_SERVER_ID = "66666666-6666-4666-8666-666666666666";

type Grant = { scope: string; selectors?: Array<Record<string, string>> };

function readGrant(resourceId: string): Grant {
  return {
    scope: "mcp:read",
    selectors: [{ resourceKind: "mcp", resourceId }],
  };
}

const readyEndpoint: WorkloadConnectionEndpoint = {
  resourceUrl: "https://app.example.com/mcp/payments",
  apiHost: "app.example.com",
  issuer: "https://auth.example.com/mcp/payments",
  tokenEndpoint: "https://auth.example.com/mcp/payments/token",
  onAuthenticationHost: true,
  grantTypesSupported: ["urn:ietf:params:oauth:grant-type:jwt-bearer"],
  workloadGrantAdvertised: true,
  ready: true,
};

const mocks = vi.hoisted(() => ({
  servers: {
    data: undefined as unknown,
    isPending: false,
    isError: false,
    refetch: vi.fn(),
  },
  details: {
    data: undefined as unknown,
    isPending: false,
    isError: false,
    refetch: vi.fn(),
  },
  detailsRequests: [] as unknown[],
  grants: [] as Grant[],
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    projects: [{ id: PROJECT_ID, name: "Payments project", slug: "payments" }],
  }),
}));
vi.mock("@/hooks/useRBAC", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/hooks/useRBAC")>()),
  useRBAC: () => ({ grants: mocks.grants, isLoading: false }),
}));
vi.mock("@gram/client/react-query/listMcpServersForOrg.js", () => ({
  useListMcpServersForOrg: () => mocks.servers,
}));
vi.mock("@gram/client/react-query/workloadConnectionDetails.js", () => ({
  useWorkloadConnectionDetails: (request: unknown) => {
    mocks.detailsRequests.push(request);
    return mocks.details;
  },
}));

function server(id: string, name: string) {
  return {
    id,
    projectId: PROJECT_ID,
    name,
    slug: name.toLowerCase(),
    networkAccessMode: "public_only",
    visibility: "private",
    createdAt: new Date("2026-09-28T00:00:00Z"),
    updatedAt: new Date("2026-09-28T00:00:00Z"),
  };
}

function details(endpoints: WorkloadConnectionEndpoint[]) {
  return { mcpServerId: SERVER_ID, mcpServerName: "Payments", endpoints };
}

beforeEach(() => {
  mocks.servers = {
    data: {
      mcpServers: [
        server(SERVER_ID, "Payments"),
        server(OTHER_SERVER_ID, "Billing"),
      ],
    },
    isPending: false,
    isError: false,
    refetch: vi.fn(),
  };
  mocks.details = {
    data: details([readyEndpoint]),
    isPending: false,
    isError: false,
    refetch: vi.fn(),
  };
  mocks.detailsRequests = [];
  mocks.grants = [readGrant(SERVER_ID)];
  Element.prototype.scrollIntoView = vi.fn<() => void>();
});
afterEach(cleanup);

function renderPicker() {
  render(
    <TooltipProvider>
      <TokenEndpointPicker />
    </TooltipProvider>,
  );
}

async function pickServer(name: string) {
  const user = userEvent.setup();
  await user.click(screen.getByRole("combobox", { name: "MCP server" }));
  await user.click(screen.getByRole("option", { name }));
}

it("labels itself as the token endpoint and fetches nothing until a server is picked", () => {
  renderPicker();

  expect(screen.getByRole("group", { name: "Token endpoint" })).toBeTruthy();
  expect(mocks.detailsRequests).toEqual([]);
  expect(
    screen.queryByRole("button", { name: "Copy token endpoint" }),
  ).toBeNull();
});

it("offers only the servers the viewer can read", async () => {
  renderPicker();

  await userEvent
    .setup()
    .click(screen.getByRole("combobox", { name: "MCP server" }));
  expect(screen.getByRole("option", { name: "Payments" })).toBeTruthy();
  expect(screen.queryByRole("option", { name: "Billing" })).toBeNull();
});

it("shows the picked server's token endpoint with a copy button", async () => {
  renderPicker();
  await pickServer("Payments");

  expect(mocks.detailsRequests.at(-1)).toEqual({ mcpServerId: SERVER_ID });
  expect(screen.getByText(readyEndpoint.tokenEndpoint).tagName).toBe("CODE");
  expect(
    screen.getByRole("button", { name: "Copy token endpoint" }),
  ).toBeTruthy();
});

it("shows the not-ready reason instead of an unusable token endpoint", async () => {
  mocks.details.data = details([
    {
      ...readyEndpoint,
      issuer: "",
      tokenEndpoint: "",
      ready: false,
      notReadyReason: "no_authorization_server",
    },
  ]);
  renderPicker();
  await pickServer("Payments");

  expect(screen.getByText("Not protected by Gram sign-in")).toBeTruthy();
  expect(
    screen.queryByRole("button", { name: "Copy token endpoint" }),
  ).toBeNull();
});

it("keeps the token endpoint but flags it when agent authorization is off", async () => {
  mocks.details.data = details([
    {
      ...readyEndpoint,
      ready: false,
      notReadyReason: "agent_rollout_disabled",
    },
  ]);
  renderPicker();
  await pickServer("Payments");

  expect(screen.getByText(readyEndpoint.tokenEndpoint)).toBeTruthy();
  expect(screen.getByText("Agent authorization is off")).toBeTruthy();
});

it("shows a token endpoint shared by several addresses once", async () => {
  mocks.details.data = details([
    readyEndpoint,
    { ...readyEndpoint, resourceUrl: "https://mcp.example.com/payments" },
  ]);
  renderPicker();
  await pickServer("Payments");

  expect(screen.getAllByText(readyEndpoint.tokenEndpoint)).toHaveLength(1);
});

it("says so when the picked server has no address", async () => {
  mocks.details.data = details([]);
  renderPicker();
  await pickServer("Payments");

  expect(screen.getByText("This MCP server has no address")).toBeTruthy();
});

it("offers a retry when the token endpoint fails to load", async () => {
  mocks.details = { ...mocks.details, data: undefined, isError: true };
  renderPicker();
  await pickServer("Payments");

  expect(screen.getByText("Couldn't load the token endpoint")).toBeTruthy();
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "Try again" }));
  expect(mocks.details.refetch).toHaveBeenCalled();
});

it("says so when the organization has no MCP servers", () => {
  mocks.servers.data = { mcpServers: [] };
  renderPicker();

  expect(screen.getByText("Create an MCP server to get one")).toBeTruthy();
  expect(screen.queryByRole("combobox", { name: "MCP server" })).toBeNull();
});

it("says so when the viewer can read none of the servers", () => {
  mocks.grants = [];
  renderPicker();

  expect(screen.getByText("No MCP servers you can view")).toBeTruthy();
  expect(screen.queryByRole("combobox", { name: "MCP server" })).toBeNull();
});

it("offers a retry when the MCP servers fail to load", async () => {
  mocks.servers = { ...mocks.servers, data: undefined, isError: true };
  renderPicker();

  expect(screen.getByText("Couldn't load MCP servers")).toBeTruthy();
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "Try again" }));
  expect(mocks.servers.refetch).toHaveBeenCalled();
});
