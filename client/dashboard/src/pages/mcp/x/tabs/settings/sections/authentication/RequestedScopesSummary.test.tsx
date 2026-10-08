import type { RemoteMcpServerClientScopes } from "@gram/client/models/components/remotemcpserverclientscopes.js";
import type { RemoteMcpServerScopes } from "@gram/client/models/components/remotemcpserverscopes.js";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";
import { RequestedScopesSummary } from "./RequestedScopesSummary";

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

function renderSummary(
  data: RemoteMcpServerScopes,
  connectedClientId: string | null = "client-1",
) {
  return render(
    <RequestedScopesSummary
      scopes={data}
      connectedClientId={connectedClientId}
    />,
  );
}

function badges(list: HTMLElement): (string | null)[] {
  return Array.from(list.querySelectorAll("li")).map((li) => li.textContent);
}

async function expectDisclaimer(text: string = DISCLAIMER): Promise<void> {
  await userEvent.hover(
    screen.getByRole("button", { name: "About requested scopes" }),
  );
  expect(await screen.findByText(text)).toBeTruthy();
}

afterEach(() => {
  cleanup();
});

describe("RequestedScopesSummary", () => {
  it("lists requested scopes without the identity scopes", async () => {
    renderSummary(
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
    expect(screen.getByText("Requested at sign-in")).toBeTruthy();
    expect(
      badges(screen.getByRole("list", { name: "Requested scopes" })),
    ).toEqual(["read", "write"]);
    await expectDisclaimer();
  });

  it("shows only the connected client", () => {
    const data = scopes([
      client(),
      client({
        clientId: "client-2",
        issuerName: "Other IdP",
        scopeSource: "client_scope",
        requestedScopes: ["admin"],
      }),
    ]);
    renderSummary(data, "client-2");
    expect(screen.getAllByText("Requested at sign-in")).toHaveLength(1);
    expect(
      badges(screen.getByRole("list", { name: "Requested scopes" })),
    ).toEqual(["admin"]);
    expect(screen.getByText("Set on this connection.")).toBeTruthy();
    cleanup();

    renderSummary(data, "client-1");
    expect(
      badges(screen.getByRole("list", { name: "Requested scopes" })),
    ).toEqual(["read", "write"]);
    expect(screen.queryByText("Set on this connection.")).toBeNull();
  });

  it.each([null, "client-9"])(
    "renders nothing when the connected client is %s",
    (connectedClientId) => {
      const { container } = renderSummary(scopes(), connectedClientId);
      expect(container.innerHTML).toBe("");
    },
  );

  it("renders nothing when the connected client requests only identity scopes", () => {
    const { container } = renderSummary(
      scopes([client({ requestedScopes: ["openid", "email", "profile"] })]),
    );
    expect(container.innerHTML).toBe("");
  });

  it.each(["issuer_omitted", "none"] as const)(
    "states the provider's defaults when %s requests nothing",
    (scopeSource) => {
      renderSummary(scopes([client({ scopeSource, requestedScopes: [] })]));
      expect(screen.getByText("Requested at sign-in")).toBeTruthy();
      expect(
        screen.getByText(
          "No scopes are requested; Acme SSO applies its defaults.",
        ),
      ).toBeTruthy();
      expect(screen.queryByRole("list")).toBeNull();
      expect(
        screen.queryByRole("button", { name: "About requested scopes" }),
      ).toBeNull();
    },
  );

  it("names an unnamed issuer by its host, else generically", async () => {
    renderSummary(scopes([client({ issuerName: undefined })]));
    await expectDisclaimer(
      "Speakeasy requests these scopes; sso.acme.test grants and enforces them. Existing connections keep their scopes until the next sign-in.",
    );
    cleanup();

    renderSummary(
      scopes([
        client({
          issuerName: undefined,
          issuerUrl: undefined,
          scopeSource: "none",
          requestedScopes: [],
        }),
      ]),
    );
    expect(
      screen.getByText(
        "No scopes are requested; the identity provider applies its defaults.",
      ),
    ).toBeTruthy();
  });

  it.each([
    ["resource_pin", "Pinned for this MCP server's URL."],
    ["client_scope", "Set on this connection."],
    ["challenge_scope", "From the MCP server's last sign-in challenge."],
    ["live_resource", "Advertised by the MCP server."],
    ["cached_resource", "Advertised by the MCP server."],
    ["issuer_override", "Set by the identity provider's override."],
    ["issuer_catalogue", "Every scope the identity provider advertises."],
  ] as const)("explains the %s source", (scopeSource, line) => {
    renderSummary(scopes([client({ scopeSource })]));
    expect(screen.getByText(line)).toBeTruthy();
  });

  it("does not repeat how many servers share a pin", () => {
    renderSummary(scopes([client()], { sharedServerCount: 2 }));
    expect(screen.getByText("Pinned for this MCP server's URL.")).toBeTruthy();
    expect(screen.queryByText(/Shared with/)).toBeNull();
  });

  it.each(["cached_resource", "issuer_override", "issuer_catalogue"] as const)(
    "warns that %s may change on the next contact",
    (scopeSource) => {
      renderSummary(
        scopes([client({ scopeSource })], {
          advertisedScopesKnown: false,
          advertisedScopes: undefined,
        }),
      );
      expect(
        screen.getByText(
          /This may change once the MCP server is next contacted\.$/,
        ),
      ).toBeTruthy();
    },
  );

  it.each(["client_scope", "challenge_scope", "resource_pin"] as const)(
    "does not warn that %s may change on the next contact",
    (scopeSource) => {
      renderSummary(
        scopes([client({ scopeSource })], {
          advertisedScopesKnown: false,
          advertisedScopes: undefined,
        }),
      );
      expect(
        screen.queryByText(/may change once the MCP server is next contacted/),
      ).toBeNull();
    },
  );

  it("does not warn for a client that does not own the resource", () => {
    renderSummary(
      scopes(
        [client({ scopeSource: "issuer_catalogue", pinWouldDecide: false })],
        { advertisedScopesKnown: false, advertisedScopes: undefined },
      ),
    );
    expect(
      screen.queryByText(/may change once the MCP server is next contacted/),
    ).toBeNull();
  });

  it("does not warn once advertised scopes are known", () => {
    renderSummary(scopes([client({ scopeSource: "cached_resource" })]));
    expect(screen.getByText("Advertised by the MCP server.")).toBeTruthy();
  });

  it("does not warn when discovery is off", () => {
    renderSummary(
      scopes([client({ scopeSource: "issuer_override" })], {
        discoveryEnabled: false,
        advertisedScopesKnown: false,
        advertisedScopes: undefined,
      }),
    );
    expect(
      screen.getByText("Set by the identity provider's override."),
    ).toBeTruthy();
  });

  it("never links to edit the scopes", () => {
    renderSummary(scopes([client()], { canPin: true }));
    expect(screen.queryByRole("link")).toBeNull();
  });
});
