import type { ReactNode } from "react";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { IdentityProviderApplication } from "@gram/client/models/components/identityproviderapplication.js";
import type { IdentityProviderConnection } from "@gram/client/models/components/identityproviderconnection.js";
import { OktaApplicationsSection } from "./okta-applications-section";

const applications = vi.hoisted(() => ({
  current: {} as {
    data?: {
      applications: IdentityProviderApplication[];
      applicationCount: number;
      readAt: Date;
      truncated: boolean;
      detail: string;
    };
    isPending: boolean;
    isFetching: boolean;
    error: unknown;
    refetch: () => unknown;
  },
}));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));
vi.mock("react-router", () => ({
  useSearchParams: () => [new URLSearchParams(), vi.fn()],
}));
vi.mock("@gram/client/react-query/listIdentityProviderApplications.js", () => ({
  useListIdentityProviderApplications: () => applications.current,
}));

// The two management calls a draft is made of, plus the rollback delete: mocked
// at the SDK rather than at the helper, so the sequence itself is under test.
const sdk = vi.hoisted(() => ({
  createRemoteServer: vi.fn(),
  createMcpServer: vi.fn(),
  deleteRemoteServer: vi.fn(),
  existingMcpServers: [] as Array<{ id: string; name?: string; slug?: string }>,
  invalidated: [] as string[],
}));

vi.mock("@/contexts/Sdk", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/contexts/Sdk")>()),
  // Called through, not captured: a test that swaps one of these between
  // renders (a retry after a failure) must reach the new one.
  useSdkClient: () => ({
    remoteMcp: {
      createServer: (...args: unknown[]) => sdk.createRemoteServer(...args),
      deleteServer: (...args: unknown[]) => sdk.deleteRemoteServer(...args),
    },
    mcpServers: {
      create: (...args: unknown[]) => sdk.createMcpServer(...args),
    },
  }),
  useProjectSlugForRequests: () => "default",
}));
vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServers: () => ({ data: { mcpServers: sdk.existingMcpServers } }),
  invalidateAllMcpServers: () => {
    sdk.invalidated.push("mcpServers");
    return Promise.resolve();
  },
}));
vi.mock("@gram/client/react-query/remoteMcpServers.js", () => ({
  invalidateAllRemoteMcpServers: () => {
    sdk.invalidated.push("remoteMcpServers");
    return Promise.resolve();
  },
}));
vi.mock("@gram/client/react-query/mcpEndpoints.js", () => ({
  invalidateAllMcpEndpoints: () => {
    sdk.invalidated.push("mcpEndpoints");
    return Promise.resolve();
  },
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: {
      x: {
        Link: ({
          params,
          children,
        }: {
          params: string[];
          children: ReactNode;
        }) => <a href={`/mcp/${params[0]}`}>{children}</a>,
      },
    },
  }),
}));

// Placeholder tenant only — never a real customer's Okta hostname.
function connection(
  overrides: Partial<IdentityProviderConnection> = {},
): IdentityProviderConnection {
  return {
    id: "conn-1",
    kind: "okta",
    tenantIdentifier: "example.okta.com",
    status: "active",
    capabilities: [],
    grantedScopes: [],
    jwksUrl:
      "https://app.example.test/.well-known/identity-provider/abc/jwks.json",
    signingKeyKid: "kid-abc123",
    createdAt: new Date("2026-09-15T10:00:00Z"),
    updatedAt: new Date("2026-09-15T10:00:00Z"),
    ...overrides,
  };
}

function group(name: string) {
  return { sourceGroupId: `00g-${name.toLowerCase()}`, name };
}

/** A catalog entry Speakeasy knows an endpoint for — placeholder host only. */
function match(
  name = "Example Chat",
): NonNullable<IdentityProviderApplication["match"]> {
  return {
    providerKey: name.toLowerCase().replace(/\s+/g, "-"),
    catalogRef: `catalog/${name.toLowerCase().replace(/\s+/g, "-")}`,
    name,
    remoteUrl: `https://mcp.${name.toLowerCase().replace(/\s+/g, "")}.test/mcp`,
    basis: "name",
    confidence: "exact",
  };
}

function application(
  overrides: Partial<IdentityProviderApplication> = {},
): IdentityProviderApplication {
  return {
    sourceApplicationId: "0oaexampleapp1",
    label: "Example Chat",
    providerStatus: "ACTIVE",
    signOnUrl: "https://chat.example.test/sso",
    logoUrl: "https://cdn.example.test/example-chat.png",
    groupAssignmentCount: 2,
    assignedGroups: [group("Engineering"), group("Support")],
    assignedGroupOverflow: 0,
    userAssignmentCount: 12,
    pickable: true,
    match: match(),
    ...overrides,
  };
}

/** An application the server will not let the reader pick, and why. */
function unpickable(
  reason: "inactive" | "no_match",
  overrides: Partial<IdentityProviderApplication> = {},
): IdentityProviderApplication {
  return application({
    pickable: false,
    unpickableReason: reason,
    match: undefined,
    providerStatus: reason === "inactive" ? "INACTIVE" : "ACTIVE",
    ...overrides,
  });
}

function withApplications(rows: IdentityProviderApplication[], detail = "") {
  applications.current = {
    data: {
      applications: rows,
      applicationCount: rows.length,
      readAt: new Date("2026-09-15T14:24:00Z"),
      truncated: false,
      detail,
    },
    isPending: false,
    isFetching: false,
    error: null,
    refetch: vi.fn(),
  };
}

function cardFor(label: string): HTMLElement {
  return screen.getByRole("checkbox", { name: label });
}

afterEach(cleanup);
beforeEach(() => {
  applications.current = {
    data: undefined,
    isPending: false,
    isFetching: false,
    error: null,
    refetch: vi.fn(),
  };
  sdk.createRemoteServer = vi.fn(async () => ({ id: "remote-1" }));
  sdk.createMcpServer = vi.fn(async () => ({
    id: "mcp-1",
    slug: "example-chat",
  }));
  sdk.deleteRemoteServer = vi.fn(async () => undefined);
  sdk.existingMcpServers = [];
  sdk.invalidated = [];
});

function pressCreate() {
  fireEvent.click(screen.getByRole("button", { name: /^Create/ }));
}

describe("OktaApplicationsSection", () => {
  it("waits until the connection is live, and asks Okta for nothing until then", () => {
    render(
      <OktaApplicationsSection
        index={4}
        connection={connection({ status: "pending" })}
      />,
    );

    const section = screen.getByRole("region", { hidden: true });
    expect(section.getAttribute("aria-disabled")).toBe("true");
    expect(section.querySelectorAll("table")).toHaveLength(0);
  });

  it("lists what Okta has: the groups by name, and the people as a count", () => {
    withApplications([
      application(),
      application({
        sourceApplicationId: "0oaexampleapp2",
        label: "Example Docs",
        signOnUrl: "https://docs.example.test/acs",
        logoUrl: undefined,
        groupAssignmentCount: 1,
        assignedGroups: [group("Sales")],
        userAssignmentCount: 1,
      }),
    ]);
    const { container } = render(
      <OktaApplicationsSection index={4} connection={connection()} />,
    );

    expect(screen.getByText("Example Chat")).toBeTruthy();
    // The host is what tells two similarly named applications apart.
    expect(screen.getByText("chat.example.test")).toBeTruthy();
    expect(screen.getByText("Engineering")).toBeTruthy();
    expect(screen.getByText("Support")).toBeTruthy();
    expect(screen.getByText("12 people")).toBeTruthy();
    // One person reads as one person, not "1 people".
    expect(screen.getByText("1 person")).toBeTruthy();
    expect(screen.getByText(/2 applications read from Okta at/)).toBeTruthy();
    const logo = container.querySelector(
      'img[src="https://cdn.example.test/example-chat.png"]',
    );
    expect(logo).toBeTruthy();
    fireEvent.error(logo!);
    expect(screen.getByText("EC")).toBeTruthy();
  });

  it("collapses the groups past the cap, counting the ones Okta had beyond the read", () => {
    withApplications([
      application({
        groupAssignmentCount: 9,
        assignedGroups: [
          group("Engineering"),
          group("Support"),
          group("Security"),
          group("Sales"),
          group("Finance"),
        ],
        assignedGroupOverflow: 4,
      }),
    ]);
    render(<OktaApplicationsSection index={4} connection={connection()} />);

    expect(screen.getByText("Engineering")).toBeTruthy();
    expect(screen.getByText("Sales")).toBeTruthy();
    // The fifth name plus the four Okta had beyond the ones read.
    expect(screen.queryByText("Finance")).toBeNull();
    expect(screen.getByText("+5 more")).toBeTruthy();
  });

  it("says a dash where Okta did not give a count, and why", () => {
    withApplications(
      [
        application({
          groupAssignmentCount: undefined,
          assignedGroups: undefined,
          assignedGroupOverflow: undefined,
          userAssignmentCount: undefined,
        }),
      ],
      "Assignment counts were omitted because the tenant has more than 50 applications.",
    );
    render(<OktaApplicationsSection index={4} connection={connection()} />);

    // A dash on its own would read as "nobody"; the server's sentence is what
    // makes it mean "not known".
    expect(screen.getByText("—")).toBeTruthy();
    expect(
      screen.getByText(/Assignment counts were omitted because/),
    ).toBeTruthy();
  });

  it("prints the sign-on host only where it differs from the tenant", () => {
    withApplications([
      application({
        label: "On the tenant",
        signOnUrl: "https://example.okta.com/app/one",
      }),
      application({
        sourceApplicationId: "0oaexampleapp2",
        label: "Somewhere else",
        signOnUrl: "https://docs.example.test/acs",
      }),
    ]);
    render(<OktaApplicationsSection index={4} connection={connection()} />);

    // Every row on a tenant signs on at the tenant, so saying so says nothing.
    expect(screen.queryByText("example.okta.com")).toBeNull();
    expect(screen.getByText("docs.example.test")).toBeTruthy();
  });

  it("starts with every pickable application picked", () => {
    withApplications([
      application(),
      application({
        sourceApplicationId: "0oaexampleapp2",
        label: "Example Docs",
      }),
      unpickable("no_match", {
        sourceApplicationId: "0oaexampleapp3",
        label: "Example Ledger",
      }),
    ]);
    render(<OktaApplicationsSection index={4} connection={connection()} />);

    // Nothing was pressed: the step opens ready to create, and the one card
    // Speakeasy has no server for is not counted in that.
    expect(cardFor("Example Chat").getAttribute("aria-checked")).toBe("true");
    expect(cardFor("Example Docs").getAttribute("aria-checked")).toBe("true");
    const create = screen.getByRole("button", { name: /^Create/ });
    expect(create.textContent).toBe("Create 2 MCP Servers");
    expect(create.hasAttribute("disabled")).toBe(false);
  });

  it("takes a card back out of the count when it is unpicked", () => {
    withApplications([
      application(),
      application({
        sourceApplicationId: "0oaexampleapp2",
        label: "Example Docs",
      }),
    ]);
    render(<OktaApplicationsSection index={4} connection={connection()} />);

    const create = () => screen.getByRole("button", { name: /^Create/ });

    fireEvent.click(cardFor("Example Chat"));
    expect(cardFor("Example Chat").getAttribute("aria-checked")).toBe("false");
    // One reads as one server, not "1 MCP Servers".
    expect(create().textContent).toBe("Create 1 MCP Server");

    fireEvent.click(cardFor("Example Docs"));
    expect(create().textContent).toBe("Create MCP Servers");
    expect(create().hasAttribute("disabled")).toBe(true);

    // The card is a toggle: pressing it again gives the pick back.
    fireEvent.click(cardFor("Example Chat"));
    expect(cardFor("Example Chat").getAttribute("aria-checked")).toBe("true");
    expect(create().textContent).toBe("Create 1 MCP Server");
  });

  it("keeps the create bar on the grid's own frame, which scrolls", () => {
    withApplications([application()]);
    const { container } = render(
      <OktaApplicationsSection index={4} connection={connection()} />,
    );

    // The applications scroll inside the frame rather than down the page, and
    // the bar is the frame's last child, so it never scrolls away from them.
    const frame = container.querySelector(".max-h-\\[32rem\\]");
    expect(frame).toBeTruthy();
    const scroller = frame!.querySelector(".overflow-y-auto");
    expect(scroller).toBeTruthy();
    expect(scroller!.querySelector(".grid")).toBeTruthy();
    const bar = frame!.lastElementChild;
    expect(bar!.contains(screen.getByRole("button", { name: /^Create/ }))).toBe(
      true,
    );
    // Not floating over the page any more.
    expect(container.querySelector(".fixed")).toBeNull();
  });

  it("says why a card cannot be picked, in the server's two reasons", () => {
    withApplications([
      application(),
      unpickable("inactive", {
        sourceApplicationId: "0oaexampleapp2",
        label: "Example Docs",
      }),
      unpickable("no_match", {
        sourceApplicationId: "0oaexampleapp3",
        label: "Example Ledger",
      }),
    ]);
    render(<OktaApplicationsSection index={4} connection={connection()} />);

    expect(screen.getByText("Inactive in Okta")).toBeTruthy();
    expect(screen.getByText("No known MCP server")).toBeTruthy();
    // Nothing to pick is nothing to press: the card is not a control at all.
    expect(screen.getAllByRole("checkbox")).toHaveLength(1);
    expect(cardFor("Example Chat")).toBeTruthy();
  });

  it("renders the applications in the order the server sent them", () => {
    withApplications([
      unpickable("no_match", {
        sourceApplicationId: "0oaexampleapp3",
        label: "Example Ledger",
      }),
      application({ sourceApplicationId: "0oaexampleapp2", label: "Zeta" }),
    ]);
    const { container } = render(
      <OktaApplicationsSection index={4} connection={connection()} />,
    );

    // The server sorts pickable first; the step does not second-guess it, so
    // whatever order it sends is the order on screen.
    const names = Array.from(
      container.querySelectorAll(".grid > *"),
      (card) => card.textContent,
    );
    expect(names[0]).toContain("Example Ledger");
    expect(names[1]).toContain("Zeta");
  });

  it("creates one MCP server per picked application, in two calls", async () => {
    withApplications([application()]);
    render(<OktaApplicationsSection index={4} connection={connection()} />);

    // Picked already: the step opens with every pickable card selected.
    pressCreate();

    await screen.findByText(/Draft created/);
    expect(sdk.createRemoteServer).toHaveBeenCalledWith(
      {
        createServerForm: {
          name: "Example Chat",
          url: "https://mcp.examplechat.test/mcp",
          transportType: "streamable-http",
        },
      },
      undefined,
      undefined,
    );
    expect(sdk.createMcpServer).toHaveBeenCalledWith(
      {
        createMcpServerForm: {
          name: "Example Chat",
          remoteMcpServerId: "remote-1",
          visibility: "private",
        },
      },
      undefined,
      undefined,
    );
    expect(sdk.deleteRemoteServer).not.toHaveBeenCalled();
    // The lists that now carry the draft are the ones refetched.
    expect(sdk.invalidated.sort()).toEqual([
      "mcpEndpoints",
      "mcpServers",
      "remoteMcpServers",
    ]);
    // The card hands over to the server's own page, where setup is finished.
    const link = screen.getByRole("link", { name: "its page" });
    expect(link.getAttribute("href")).toBe("/mcp/example-chat");
    // A created card is no longer something to pick, and pressing the bar
    // again would create nothing.
    expect(screen.queryAllByRole("checkbox")).toHaveLength(0);
    expect(
      screen.getByRole("button", { name: /^Create/ }).hasAttribute("disabled"),
    ).toBe(true);
  });

  it("deletes the remote server again when linking an MCP server fails", async () => {
    sdk.createMcpServer = vi.fn(async () => {
      throw new Error("mcp server name already taken");
    });
    withApplications([application()]);
    render(<OktaApplicationsSection index={4} connection={connection()} />);

    // Picked already: the step opens with every pickable card selected.
    pressCreate();

    await screen.findByText("mcp server name already taken");
    expect(sdk.deleteRemoteServer).toHaveBeenCalledWith(
      { id: "remote-1" },
      undefined,
      undefined,
    );
    // Nothing was made, so nothing needed refetching.
    expect(sdk.invalidated).toEqual([]);

    // Retry is the card's own, and it runs the pair again.
    sdk.createMcpServer = vi.fn(async () => ({ id: "mcp-1", slug: "chat" }));
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await screen.findByText(/Draft created/);
    expect(sdk.createRemoteServer).toHaveBeenCalledTimes(2);
  });

  it("says so when the rollback fails too, because the leftover needs a hand", async () => {
    sdk.createMcpServer = vi.fn(async () => {
      throw new Error("link failed");
    });
    sdk.deleteRemoteServer = vi.fn(async () => {
      throw new Error("delete failed");
    });
    withApplications([application()]);
    render(<OktaApplicationsSection index={4} connection={connection()} />);

    // Picked already: the step opens with every pickable card selected.
    pressCreate();

    const message = await screen.findByText(/Delete it manually/);
    expect(message.textContent).toContain("remote-1");
    expect(message.textContent).toContain("link failed");
    expect(message.textContent).toContain("delete failed");
  });

  it("does not send an endpoint the catalog gave in a form that cannot work", async () => {
    withApplications([
      application({ match: { ...match(), remoteUrl: "mcp.example.test/mcp" } }),
    ]);
    render(<OktaApplicationsSection index={4} connection={connection()} />);

    // Picked already: the step opens with every pickable card selected.
    pressCreate();

    await screen.findByText(/no usable endpoint for this application/);
    expect(sdk.createRemoteServer).not.toHaveBeenCalled();
  });

  it("leaves an application the project already has alone", async () => {
    sdk.existingMcpServers = [
      { id: "mcp-existing", name: "example chat", slug: "example-chat" },
    ];
    withApplications([application()]);
    render(<OktaApplicationsSection index={4} connection={connection()} />);

    // Picked already: the step opens with every pickable card selected.
    pressCreate();

    await screen.findByText(/Already an MCP server in this project/);
    expect(sdk.createRemoteServer).not.toHaveBeenCalled();
    expect(
      screen.getByRole("link", { name: "its page" }).getAttribute("href"),
    ).toBe("/mcp/example-chat");
  });

  it("offers a refresh that re-reads Okta", () => {
    const refetch = vi.fn();
    withApplications([application()]);
    applications.current.refetch = refetch;
    render(<OktaApplicationsSection index={4} connection={connection()} />);

    fireEvent.click(screen.getByRole("button", { name: /refresh/i }));
    expect(refetch).toHaveBeenCalledOnce();
  });

  it("asks Okta for nothing the reader types: the step has no search box", () => {
    withApplications([application()]);
    render(<OktaApplicationsSection index={4} connection={connection()} />);

    expect(screen.queryByPlaceholderText(/search/i)).toBeNull();
  });
});
