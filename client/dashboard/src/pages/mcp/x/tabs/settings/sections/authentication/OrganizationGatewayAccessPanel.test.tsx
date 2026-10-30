import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  OrganizationGatewayAccessPanel,
  type GatewaySignInClient,
} from "./OrganizationGatewayAccessPanel";

const GATEWAY_ISSUER = "11111111-1111-4111-8111-111111111111";
const SERVER_ISSUER = "22222222-2222-4222-8222-222222222222";

const mocks = vi.hoisted(() => ({
  gateway: vi.fn(),
  attach: vi.fn(),
  detach: vi.fn(),
  handleError: vi.fn(),
}));

vi.mock("@gram/client/react-query/organizationGateway.js", () => ({
  useOrganizationGateway: () => ({ data: mocks.gateway() }),
}));
vi.mock("@gram/client/react-query/attachUserSessionIssuer.js", () => ({
  useAttachUserSessionIssuerMutation: () => ({ mutateAsync: mocks.attach }),
}));
vi.mock("@gram/client/react-query/detachUserSessionIssuer.js", () => ({
  useDetachUserSessionIssuerMutation: () => ({ mutateAsync: mocks.detach }),
}));
vi.mock("@gram/client/react-query/remoteSessionClients.js", () => ({
  invalidateAllRemoteSessionClients: vi.fn(),
}));
vi.mock("@gram/client/react-query/serverClientAssociations.js", () => ({
  invalidateAllServerClientAssociations: vi.fn(),
}));
vi.mock("@/lib/errors", () => ({ handleError: mocks.handleError }));

function client(
  id: string,
  userSessionIssuerIds: string[],
  otherServers = 0,
): GatewaySignInClient {
  return {
    client: { id, userSessionIssuerIds } as unknown as RemoteSessionClient,
    otherServers,
  };
}

function renderPanel(
  props: Partial<Parameters<typeof OrganizationGatewayAccessPanel>[0]> = {},
  queryClient: QueryClient = new QueryClient(),
): void {
  render(
    <QueryClientProvider client={queryClient}>
      <OrganizationGatewayAccessPanel
        serverUserSessionIssuerId={SERVER_ISSUER}
        clients={[client("client-a", [SERVER_ISSUER])]}
        preferredClientId={null}
        canWrite
        {...props}
      />
    </QueryClientProvider>,
  );
}

function gatewaySwitch(): HTMLElement {
  return screen.getByRole("switch", {
    name: "Offer on the organization gateway",
  });
}

beforeEach(() => {
  mocks.gateway.mockReturnValue({
    available: true,
    userSessionIssuerId: GATEWAY_ISSUER,
  });
  mocks.attach.mockResolvedValue({});
  mocks.detach.mockResolvedValue({});
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("OrganizationGatewayAccessPanel", () => {
  function attachRequest(id: string) {
    return {
      request: {
        attachUserSessionIssuerForm: {
          id,
          userSessionIssuerId: GATEWAY_ISSUER,
        },
      },
    };
  }

  it("offers a server whose sign-in is not on the gateway yet", async () => {
    renderPanel();

    expect(gatewaySwitch().getAttribute("aria-checked")).toBe("false");
    fireEvent.click(gatewaySwitch());

    await waitFor(() => {
      expect(mocks.attach).toHaveBeenCalledWith(attachRequest("client-a"));
    });
    expect(mocks.detach).not.toHaveBeenCalled();
  });

  it("offers one client when the server has several, preferring the connected one", async () => {
    renderPanel({
      clients: [
        client("client-a", [SERVER_ISSUER]),
        client("client-b", [SERVER_ISSUER]),
      ],
      preferredClientId: "client-b",
    });

    fireEvent.click(gatewaySwitch());

    await waitFor(() => expect(mocks.attach).toHaveBeenCalledTimes(1));
    expect(mocks.attach).toHaveBeenCalledWith(attachRequest("client-b"));
  });

  it("reads as offered when any client is on the gateway", () => {
    renderPanel({
      clients: [
        client("client-a", [SERVER_ISSUER]),
        client("client-b", [SERVER_ISSUER, GATEWAY_ISSUER]),
      ],
    });

    expect(gatewaySwitch().getAttribute("aria-checked")).toBe("true");
    expect(
      screen.getByText("Offered on the organization gateway"),
    ).toBeDefined();
  });

  it("withdraws every client that is on the gateway", async () => {
    renderPanel({
      clients: [
        client("client-a", [SERVER_ISSUER, GATEWAY_ISSUER]),
        client("client-b", [SERVER_ISSUER]),
        client("client-c", [SERVER_ISSUER, GATEWAY_ISSUER]),
      ],
    });

    fireEvent.click(gatewaySwitch());

    await waitFor(() => expect(mocks.detach).toHaveBeenCalledTimes(2));
    expect(mocks.detach).toHaveBeenCalledWith(attachRequest("client-a"));
    expect(mocks.detach).toHaveBeenCalledWith(attachRequest("client-c"));
    expect(mocks.attach).not.toHaveBeenCalled();
  });

  it("updates the cached client lists in place instead of refetching them", async () => {
    const queryClient = new QueryClient();
    const listKey = [
      "@gram/client",
      "remoteSessionClients",
      "list",
      "infinite",
      { userSessionIssuerId: SERVER_ISSUER },
    ];
    queryClient.setQueryData(listKey, {
      pages: [
        { result: { items: [client("client-a", [SERVER_ISSUER]).client] } },
      ],
      pageParams: [undefined],
    });
    const updated = client("client-a", [SERVER_ISSUER, GATEWAY_ISSUER]).client;
    mocks.attach.mockResolvedValue(updated);
    renderPanel({}, queryClient);

    fireEvent.click(gatewaySwitch());

    await waitFor(() => {
      expect(
        queryClient.getQueryData<{
          pages: { result: { items: RemoteSessionClient[] } }[];
        }>(listKey)?.pages[0]?.result.items[0]?.userSessionIssuerIds,
      ).toEqual([SERVER_ISSUER, GATEWAY_ISSUER]);
    });
    expect(queryClient.getQueryState(listKey)?.isInvalidated).toBe(false);
  });

  it("reports a refused change", async () => {
    const refusal = new Error("conflict");
    mocks.attach.mockRejectedValue(refusal);
    renderPanel();

    fireEvent.click(gatewaySwitch());

    await waitFor(() => {
      expect(mocks.handleError).toHaveBeenCalledWith(refusal, {
        title: "Couldn't add this server to the organization gateway",
      });
    });
  });

  it("names the other servers the client being offered signs in", () => {
    renderPanel({
      clients: [
        client("client-a", [SERVER_ISSUER], 0),
        client("client-b", [SERVER_ISSUER], 2),
      ],
      preferredClientId: "client-b",
    });

    expect(
      screen.getByText(
        "This sign-in is shared, so the change also applies to 2 other MCP servers.",
      ),
    ).toBeDefined();
  });

  it("states a lower bound when several shared clients would be withdrawn", () => {
    renderPanel({
      clients: [
        client("client-a", [SERVER_ISSUER, GATEWAY_ISSUER], 1),
        client("client-b", [SERVER_ISSUER, GATEWAY_ISSUER], 3),
      ],
    });

    expect(
      screen.getByText(
        "This sign-in is shared, so the change also applies to at least 3 other MCP servers.",
      ),
    ).toBeDefined();
  });

  it("is read-only without edit access", () => {
    renderPanel({ canWrite: false });

    expect(gatewaySwitch().hasAttribute("disabled")).toBe(true);
    expect(
      screen.getByText("Only people who can edit this server can change this."),
    ).toBeDefined();
  });

  it.each([
    ["the gateway is unavailable", { available: false }, {}],
    [
      "the server already signs in through the gateway's issuer",
      { available: true, userSessionIssuerId: GATEWAY_ISSUER },
      { serverUserSessionIssuerId: GATEWAY_ISSUER },
    ],
    [
      "the server has no per-person sign-in",
      { available: true, userSessionIssuerId: GATEWAY_ISSUER },
      { clients: [] },
    ],
  ])("renders nothing when %s", (_, gateway, props) => {
    mocks.gateway.mockReturnValue(gateway);
    renderPanel(props);

    expect(screen.queryByRole("switch")).toBeNull();
  });
});
