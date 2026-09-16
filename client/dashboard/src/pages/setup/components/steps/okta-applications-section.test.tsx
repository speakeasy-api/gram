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
    ...overrides,
  };
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
});

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

  it("picks cards, and counts the picks on the one button", () => {
    withApplications([
      application(),
      application({
        sourceApplicationId: "0oaexampleapp2",
        label: "Example Docs",
      }),
    ]);
    render(<OktaApplicationsSection index={4} connection={connection()} />);

    const create = () => screen.getByRole("button", { name: /^Create/ });
    expect(create().hasAttribute("disabled")).toBe(true);
    expect(create().textContent).toBe("Create MCP Servers");

    fireEvent.click(cardFor("Example Chat"));
    expect(cardFor("Example Chat").getAttribute("aria-checked")).toBe("true");
    // One reads as one server, not "1 MCP Servers".
    expect(create().textContent).toBe("Create 1 MCP Server");
    expect(create().hasAttribute("disabled")).toBe(false);

    fireEvent.click(cardFor("Example Docs"));
    expect(create().textContent).toBe("Create 2 MCP Servers");

    // The card is a toggle: pressing it again gives the pick back.
    fireEvent.click(cardFor("Example Chat"));
    expect(cardFor("Example Chat").getAttribute("aria-checked")).toBe("false");
    expect(create().textContent).toBe("Create 1 MCP Server");
  });

  it("says why a card cannot be picked, and sorts it to the end", () => {
    withApplications([
      application({
        sourceApplicationId: "0oaexampleapp2",
        label: "Example Docs",
        providerStatus: "INACTIVE",
      }),
      application(),
    ]);
    const { container } = render(
      <OktaApplicationsSection index={4} connection={connection()} />,
    );

    expect(screen.getByText("Inactive in Okta")).toBeTruthy();
    // Nothing to pick is nothing to press: the card is not a control at all.
    expect(screen.getAllByRole("checkbox")).toHaveLength(1);
    expect(cardFor("Example Chat")).toBeTruthy();

    const names = Array.from(
      container.querySelectorAll(".grid > *"),
      (card) => card.textContent,
    );
    expect(names[0]).toContain("Example Chat");
    expect(names[1]).toContain("Example Docs");
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
