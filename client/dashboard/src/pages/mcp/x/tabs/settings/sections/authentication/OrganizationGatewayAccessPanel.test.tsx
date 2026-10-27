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
import { OrganizationGatewayAccessPanel } from "./OrganizationGatewayAccessPanel";

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
): RemoteSessionClient {
  return { id, userSessionIssuerIds } as unknown as RemoteSessionClient;
}

function renderPanel(
  props: Partial<Parameters<typeof OrganizationGatewayAccessPanel>[0]> = {},
): void {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <OrganizationGatewayAccessPanel
        serverUserSessionIssuerId={SERVER_ISSUER}
        clients={[client("client-a", [SERVER_ISSUER])]}
        otherServersOnClient={0}
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
  it("offers a server whose sign-in is not on the gateway yet", async () => {
    renderPanel();

    expect(gatewaySwitch().getAttribute("aria-checked")).toBe("false");
    fireEvent.click(gatewaySwitch());

    await waitFor(() => {
      expect(mocks.attach).toHaveBeenCalledWith({
        request: {
          attachUserSessionIssuerForm: {
            id: "client-a",
            userSessionIssuerId: GATEWAY_ISSUER,
          },
        },
      });
    });
    expect(mocks.detach).not.toHaveBeenCalled();
  });

  it("stops offering a server already on the gateway", async () => {
    renderPanel({
      clients: [client("client-a", [SERVER_ISSUER, GATEWAY_ISSUER])],
    });

    expect(gatewaySwitch().getAttribute("aria-checked")).toBe("true");
    fireEvent.click(gatewaySwitch());

    await waitFor(() => {
      expect(mocks.detach).toHaveBeenCalledWith({
        request: {
          attachUserSessionIssuerForm: {
            id: "client-a",
            userSessionIssuerId: GATEWAY_ISSUER,
          },
        },
      });
    });
    expect(mocks.attach).not.toHaveBeenCalled();
  });

  it("attaches only the clients that are missing", async () => {
    renderPanel({
      clients: [
        client("client-a", [SERVER_ISSUER, GATEWAY_ISSUER]),
        client("client-b", [SERVER_ISSUER]),
      ],
    });

    expect(gatewaySwitch().getAttribute("aria-checked")).toBe("false");
    fireEvent.click(gatewaySwitch());

    await waitFor(() => expect(mocks.attach).toHaveBeenCalledTimes(1));
    expect(mocks.attach).toHaveBeenCalledWith({
      request: {
        attachUserSessionIssuerForm: {
          id: "client-b",
          userSessionIssuerId: GATEWAY_ISSUER,
        },
      },
    });
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

  it("names the other servers that share the sign-in", () => {
    renderPanel({ otherServersOnClient: 2 });

    expect(screen.getByText(/2 other MCP servers/)).toBeDefined();
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
