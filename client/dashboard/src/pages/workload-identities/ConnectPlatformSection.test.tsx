import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { WorkloadConnectionEndpoint } from "@gram/client/models/components/workloadconnectionendpoint.js";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { ConnectPlatformSection } from "./ConnectPlatformSection";

const PROJECT_ID = "44444444-4444-4444-8444-444444444444";
const SERVER_ID = "55555555-5555-4555-8555-555555555555";
const OTHER_SERVER_ID = "66666666-6666-4666-8666-666666666666";
const TOOLSET_SERVER_ID = "77777777-7777-4777-8777-777777777777";
const TOOLSET_ID = "88888888-8888-4888-8888-888888888888";

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
  grantTypesSupported: [
    "authorization_code",
    "refresh_token",
    "urn:ietf:params:oauth:grant-type:jwt-bearer",
  ],
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

function server(id: string, name: string, toolsetId?: string) {
  return {
    id,
    projectId: PROJECT_ID,
    name,
    slug: name.toLowerCase(),
    toolsetId,
    networkAccessMode: "public_only",
    visibility: "private",
    createdAt: new Date("2026-09-28T00:00:00Z"),
    updatedAt: new Date("2026-09-28T00:00:00Z"),
  };
}

beforeEach(() => {
  mocks.servers = {
    data: { mcpServers: [server(SERVER_ID, "Payments")] },
    isPending: false,
    isError: false,
    refetch: vi.fn(),
  };
  mocks.details = {
    data: {
      mcpServerId: SERVER_ID,
      mcpServerName: "Payments",
      endpoints: [readyEndpoint],
    },
    isPending: false,
    isError: false,
    refetch: vi.fn(),
  };
  mocks.detailsRequests = [];
  mocks.grants = [readGrant(SERVER_ID)];
  // Radix scrolls the selected option into view in browsers.
  Element.prototype.scrollIntoView = vi.fn<() => void>();
});
afterEach(cleanup);

// The app root provides tooltips; the copy buttons need one here too.
function renderSection() {
  render(
    <TooltipProvider>
      <ConnectPlatformSection />
    </TooltipProvider>,
  );
}

async function pickServer(name: string) {
  const user = userEvent.setup();
  await user.click(screen.getByRole("combobox", { name: "MCP server" }));
  await user.click(screen.getByRole("option", { name }));
}

function valueOf(label: string): string | null | undefined {
  const heading = screen.getByText(label);
  return heading.parentElement?.querySelector("code")?.textContent;
}

it("shows nothing to copy until a server is picked", () => {
  renderSection();

  expect(screen.queryByText("Token endpoint")).toBeNull();
  expect(mocks.detailsRequests).toEqual([]);
});

it("shows the picked server's connection values", async () => {
  renderSection();
  await pickServer("Payments");

  expect(mocks.detailsRequests.at(-1)).toEqual({ mcpServerId: SERVER_ID });
  expect(valueOf("Token endpoint")).toBe(readyEndpoint.tokenEndpoint);
  expect(valueOf("Authorization server issuer")).toBe(readyEndpoint.issuer);
  expect(valueOf("Resource (MCP server URL)")).toBe(readyEndpoint.resourceUrl);
  expect(valueOf("API host")).toBe("app.example.com");
  expect(
    screen.getByText(/deliberately a different host from the MCP server/),
  ).toBeTruthy();
  expect(
    screen.getByText(/must be exactly this issuer or the token endpoint URL/),
  ).toBeTruthy();
  expect(screen.queryByText("Not ready: exchanges will fail")).toBeNull();
});

it("hides a picked server's values once the viewer can no longer read it", async () => {
  mocks.grants = [readGrant(SERVER_ID), readGrant(OTHER_SERVER_ID)];
  const page = () => (
    <TooltipProvider>
      <ConnectPlatformSection />
    </TooltipProvider>
  );
  const { rerender } = render(page());
  await pickServer("Payments");
  expect(valueOf("Token endpoint")).toBe(readyEndpoint.tokenEndpoint);

  mocks.grants = [readGrant(OTHER_SERVER_ID)];
  rerender(page());

  expect(screen.queryByText("Token endpoint")).toBeNull();
});

it("warns when the workload grant is not advertised", async () => {
  mocks.details.data = {
    mcpServerId: SERVER_ID,
    mcpServerName: "Payments",
    endpoints: [
      {
        ...readyEndpoint,
        grantTypesSupported: ["authorization_code", "refresh_token"],
        workloadGrantAdvertised: false,
        ready: false,
        notReadyReason: "workload_grant_unavailable",
      },
    ],
  };
  renderSection();
  await pickServer("Payments");

  const warning = screen.getByText("Not ready: exchanges will fail");
  expect(
    within(warning.parentElement as HTMLElement).getByText(
      /doesn't advertise the jwt-bearer grant/,
    ),
  ).toBeTruthy();
});

it("shows no token endpoint when Gram is not the authorization server", async () => {
  mocks.details.data = {
    mcpServerId: SERVER_ID,
    mcpServerName: "Payments",
    endpoints: [
      {
        ...readyEndpoint,
        issuer: "",
        tokenEndpoint: "",
        onAuthenticationHost: false,
        grantTypesSupported: [],
        workloadGrantAdvertised: false,
        ready: false,
        notReadyReason: "no_authorization_server",
      },
    ],
  };
  renderSection();
  await pickServer("Payments");

  expect(
    screen.getByText(/isn't this MCP server's authorization server/),
  ).toBeTruthy();
  expect(screen.queryByText("Token endpoint")).toBeNull();
  expect(valueOf("Resource (MCP server URL)")).toBe(readyEndpoint.resourceUrl);
});

it("says so when the organization has no MCP servers", () => {
  mocks.servers.data = { mcpServers: [] };
  renderSection();

  expect(screen.getByText("No MCP servers yet")).toBeTruthy();
  expect(screen.queryByRole("combobox", { name: "MCP server" })).toBeNull();
});

it("offers a retry when the MCP servers fail to load", () => {
  mocks.servers.isError = true;
  mocks.servers.data = undefined;
  renderSection();

  screen.getByRole("button", { name: "Try again" }).click();
  expect(mocks.servers.refetch).toHaveBeenCalled();
});

it("offers a retry when the connection details fail to load", async () => {
  mocks.details.isError = true;
  mocks.details.data = undefined;
  renderSection();
  await pickServer("Payments");

  expect(screen.getByText("Couldn't load the connection details")).toBeTruthy();
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "Try again" }));
  expect(mocks.details.refetch).toHaveBeenCalled();
});

it("offers only the servers the viewer can read", async () => {
  mocks.servers.data = {
    mcpServers: [
      server(SERVER_ID, "Payments"),
      server(OTHER_SERVER_ID, "Billing"),
      server(TOOLSET_SERVER_ID, "Hosted", TOOLSET_ID),
    ],
  };
  // A toolset-backed server's read grant names its toolset, not the server.
  mocks.grants = [readGrant(SERVER_ID), readGrant(TOOLSET_ID)];
  renderSection();

  await userEvent
    .setup()
    .click(screen.getByRole("combobox", { name: "MCP server" }));
  expect(screen.getByRole("option", { name: "Payments" })).toBeTruthy();
  expect(screen.getByRole("option", { name: "Hosted" })).toBeTruthy();
  expect(screen.queryByRole("option", { name: "Billing" })).toBeNull();
});

it("says so when the viewer can read none of the organization's servers", () => {
  mocks.grants = [readGrant(OTHER_SERVER_ID)];
  renderSection();

  expect(screen.getByText("No MCP servers you can view")).toBeTruthy();
  expect(screen.queryByRole("combobox", { name: "MCP server" })).toBeNull();
});

it("says so when the picked server has no address", async () => {
  mocks.details.data = {
    mcpServerId: SERVER_ID,
    mcpServerName: "Payments",
    endpoints: [],
  };
  renderSection();
  await pickServer("Payments");

  expect(screen.getByText("This MCP server has no address")).toBeTruthy();
  expect(screen.queryByText("Resource (MCP server URL)")).toBeNull();
});

it("shows no token endpoint when the server is not publicly reachable", async () => {
  mocks.details.data = {
    mcpServerId: SERVER_ID,
    mcpServerName: "Payments",
    endpoints: [
      {
        ...readyEndpoint,
        issuer: "",
        tokenEndpoint: "",
        onAuthenticationHost: false,
        grantTypesSupported: [],
        workloadGrantAdvertised: false,
        ready: false,
        notReadyReason: "not_publicly_reachable",
      },
    ],
  };
  renderSection();
  await pickServer("Payments");

  expect(screen.getByText(/can't be reached publicly/)).toBeTruthy();
  expect(screen.queryByText("Token endpoint")).toBeNull();
  expect(screen.queryByText("Authorization server issuer")).toBeNull();
  expect(valueOf("Resource (MCP server URL)")).toBe(readyEndpoint.resourceUrl);
});

it("keeps the values when agent authorization is off", async () => {
  mocks.details.data = {
    mcpServerId: SERVER_ID,
    mcpServerName: "Payments",
    endpoints: [
      {
        ...readyEndpoint,
        ready: false,
        notReadyReason: "agent_rollout_disabled",
      },
    ],
  };
  renderSection();
  await pickServer("Payments");

  const warning = screen.getByText("Not ready: exchanges will fail");
  expect(
    within(warning.parentElement as HTMLElement).getByText(
      /Agent authorization isn't enabled for this organization/,
    ),
  ).toBeTruthy();
  expect(valueOf("Token endpoint")).toBe(readyEndpoint.tokenEndpoint);
  expect(valueOf("Authorization server issuer")).toBe(readyEndpoint.issuer);
});
