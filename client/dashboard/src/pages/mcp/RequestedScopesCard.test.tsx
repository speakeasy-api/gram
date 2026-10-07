import type { RemoteMcpServerClientScopes } from "@gram/client/models/components/remotemcpserverclientscopes.js";
import type { RemoteMcpServerScopes } from "@gram/client/models/components/remotemcpserverscopes.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MCPTeamAccessTab } from "./MCPTeamAccessTab";
import { RequestedScopesCard } from "./RequestedScopesCard";

const mocks = vi.hoisted(() => ({
  scopes: vi.fn(),
}));

vi.mock("@gram/client/react-query/getRemoteMcpServerScopes.js", () => ({
  useGetRemoteMcpServerScopes: (...args: unknown[]) => mocks.scopes(...args),
}));

vi.mock("@gram/client/react-query/resourceAudience.js", () => ({
  useResourceAudience: () => ({
    data: { entries: [], version: "" },
    isLoading: false,
    isError: false,
  }),
}));

vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({ data: { members: [] } }),
}));

vi.mock("./access/CheckAccess", () => ({ CheckAccess: () => null }));
vi.mock("./access/ManageAccess", () => ({ ManageAccess: () => null }));

const EDIT_HREF = "/mcp/x/linear/settings#authentication";
const DISCLAIMER =
  "Speakeasy requests these scopes; Acme SSO grants and enforces them. Existing connections keep their scopes until the next sign-in.";

function client(
  overrides: Partial<RemoteMcpServerClientScopes> = {},
): RemoteMcpServerClientScopes {
  return {
    clientId: "client-1",
    issuerName: "Acme SSO",
    issuerUrl: "https://sso.acme.test/oauth",
    scopeSource: "resource_pin",
    requestedScopes: ["read", "write", "openid", "offline_access"],
    unadvertisedPinnedScopes: [],
    pinWouldDecide: true,
    ...overrides,
  };
}

function scopes(
  clients: RemoteMcpServerClientScopes[] = [client()],
  overrides: Partial<RemoteMcpServerScopes> = {},
): RemoteMcpServerScopes {
  return {
    resourceUrl: "https://mcp.example/mcp",
    pinnedScopes: ["read", "write"],
    advertisedScopesKnown: true,
    advertisedScopes: ["read", "write"],
    challengeScopes: [],
    discoveryEnabled: true,
    sharedServerCount: 0,
    canPin: false,
    clients,
    ...overrides,
  };
}

function loaded(data: RemoteMcpServerScopes): void {
  mocks.scopes.mockReturnValue({
    data,
    error: null,
    isError: false,
    isLoading: false,
  });
}

function renderCard(editHref: string | undefined = EDIT_HREF) {
  return render(
    <MemoryRouter>
      <RequestedScopesCard mcpServerId="mcp-server-1" editHref={editHref} />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  loaded(scopes());
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("MCPTeamAccessTab", () => {
  it("shows the requested scopes for a remote-backed server", () => {
    render(
      <MemoryRouter>
        <MCPTeamAccessTab
          resourceId="mcp-server-1"
          serverName="Linear"
          requestedScopes={{ editScopesHref: EDIT_HREF }}
        />
      </MemoryRouter>,
    );
    expect(screen.getByText("Scopes requested from Acme SSO")).toBeTruthy();
    expect(mocks.scopes).toHaveBeenCalledWith(
      { mcpServerId: "mcp-server-1" },
      undefined,
      expect.objectContaining({ throwOnError: false }),
    );
  });

  it("neither fetches nor renders scopes for other servers", () => {
    render(
      <MemoryRouter>
        <MCPTeamAccessTab resourceId="mcp-server-1" serverName="Toolset" />
      </MemoryRouter>,
    );
    expect(screen.queryByText(/Scopes requested from/)).toBeNull();
    expect(mocks.scopes).not.toHaveBeenCalled();
  });
});

describe("RequestedScopesCard", () => {
  it("lists requested scopes without the identity scopes", () => {
    loaded(
      scopes([
        client({
          requestedScopes: [
            "openid",
            "read",
            "email",
            "profile",
            "write",
            "offline_access",
          ],
        }),
      ]),
    );
    renderCard();
    expect(screen.getByText("Scopes requested from Acme SSO")).toBeTruthy();
    const list = screen.getByRole("list", { name: "Requested scopes" });
    expect(
      Array.from(list.querySelectorAll("li")).map((li) => li.textContent),
    ).toEqual(["read", "write"]);
    expect(screen.getByText(DISCLAIMER)).toBeTruthy();
  });

  it("hides a client with one scope or fewer left", () => {
    loaded(
      scopes([
        client({ requestedScopes: ["read", "openid"] }),
        client({
          clientId: "client-2",
          issuerName: "Other IdP",
          requestedScopes: ["openid", "email"],
        }),
      ]),
    );
    const { container } = renderCard();
    expect(container.innerHTML).toBe("");
  });

  it("shows only the clients that qualify", () => {
    loaded(
      scopes([
        client({ requestedScopes: ["read"] }),
        client({ clientId: "client-2", issuerName: "Other IdP" }),
      ]),
    );
    renderCard();
    expect(screen.queryByText("Scopes requested from Acme SSO")).toBeNull();
    expect(screen.getByText("Scopes requested from Other IdP")).toBeTruthy();
  });

  it.each(["issuer_omitted", "none"] as const)(
    "states the provider's defaults when %s requests nothing",
    (scopeSource) => {
      loaded(scopes([client({ scopeSource, requestedScopes: [] })]));
      renderCard();
      expect(
        screen.getByText("No scopes requested from Acme SSO"),
      ).toBeTruthy();
      expect(screen.getByText("Acme SSO applies its defaults.")).toBeTruthy();
      expect(screen.queryByRole("list")).toBeNull();
      expect(screen.queryByText(DISCLAIMER)).toBeNull();
    },
  );

  it("names an unnamed issuer by its host, else generically", () => {
    loaded(scopes([client({ issuerName: undefined })]));
    renderCard();
    expect(
      screen.getByText("Scopes requested from sso.acme.test"),
    ).toBeTruthy();
    cleanup();

    loaded(
      scopes([
        client({
          issuerName: undefined,
          issuerUrl: undefined,
          scopeSource: "none",
          requestedScopes: [],
        }),
      ]),
    );
    renderCard();
    expect(
      screen.getByText("No scopes requested from the identity provider"),
    ).toBeTruthy();
    expect(
      screen.getByText("The identity provider applies its defaults."),
    ).toBeTruthy();
  });

  it("tells same-named issuers apart by URL", () => {
    loaded(
      scopes([
        client(),
        client({
          clientId: "client-2",
          issuerUrl: "https://login.acme.test/",
        }),
      ]),
    );
    renderCard();
    expect(
      screen.getByText("Scopes requested from Acme SSO (sso.acme.test/oauth)"),
    ).toBeTruthy();
    expect(
      screen.getByText("Scopes requested from Acme SSO (login.acme.test)"),
    ).toBeTruthy();
  });

  it.each([
    ["resource_pin", "Pinned for this server's URL."],
    ["client_scope", "Set on this connection."],
    ["challenge_scope", "From the server's last sign-in challenge."],
    ["live_resource", "Advertised by the server."],
    ["cached_resource", "Advertised by the server."],
    ["issuer_override", "Set by the identity provider's override."],
    ["issuer_catalogue", "Every scope the identity provider advertises."],
  ] as const)("explains the %s source", (scopeSource, line) => {
    loaded(scopes([client({ scopeSource })]));
    renderCard();
    expect(screen.getByText(line)).toBeTruthy();
    expect(screen.getByText(DISCLAIMER)).toBeTruthy();
  });

  it("says how many servers share a pin", () => {
    loaded(scopes([client()], { sharedServerCount: 2 }));
    renderCard();
    expect(
      screen.getByText(
        "Pinned for this server's URL. Shared with 2 other MCP servers on the same URL.",
      ),
    ).toBeTruthy();
  });

  it("warns that unread advertised scopes may change the request", () => {
    loaded(
      scopes([client({ scopeSource: "issuer_override" })], {
        advertisedScopesKnown: false,
        advertisedScopes: undefined,
      }),
    );
    renderCard();
    expect(
      screen.getByText(
        "Set by the identity provider's override. This may change once the MCP server is next contacted.",
      ),
    ).toBeTruthy();
  });

  it("links to the scope settings only when the caller can pin and the pin decides", () => {
    renderCard();
    expect(screen.queryByRole("link", { name: "Edit scopes" })).toBeNull();
    cleanup();

    loaded(scopes([client()], { canPin: true }));
    renderCard();
    expect(
      screen.getByRole("link", { name: "Edit scopes" }).getAttribute("href"),
    ).toBe(EDIT_HREF);
    cleanup();

    loaded(
      scopes([client({ scopeSource: "client_scope", pinWouldDecide: false })], {
        canPin: true,
      }),
    );
    renderCard();
    expect(screen.queryByRole("link", { name: "Edit scopes" })).toBeNull();
    cleanup();

    loaded(
      scopes([client({ scopeSource: "live_resource", pinWouldDecide: true })], {
        canPin: true,
      }),
    );
    renderCard();
    expect(screen.getByRole("link", { name: "Edit scopes" })).toBeTruthy();
  });

  it("renders nothing while loading", () => {
    mocks.scopes.mockReturnValue({
      data: undefined,
      error: null,
      isError: false,
      isLoading: true,
    });
    const { container } = renderCard();
    expect(container.innerHTML).toBe("");
  });

  it("renders nothing when the scopes fail to load", () => {
    mocks.scopes.mockReturnValue({
      data: scopes(),
      error: new Error("boom"),
      isError: true,
      isLoading: false,
    });
    const { container } = renderCard();
    expect(container.innerHTML).toBe("");
  });

  it("explains a refusal to read a sibling server", () => {
    mocks.scopes.mockReturnValue({
      data: undefined,
      error: new GramError("permission denied", {
        response: new Response(null, { status: 403 }),
        request: new Request("https://app.getgram.ai/rpc/example"),
        body: "",
      }),
      isError: true,
      isLoading: false,
    });
    renderCard();
    expect(
      screen.getByText(
        "Requested scopes are shared by every MCP server that uses this URL. You need access to all of them to see them.",
      ),
    ).toBeTruthy();
  });
});
