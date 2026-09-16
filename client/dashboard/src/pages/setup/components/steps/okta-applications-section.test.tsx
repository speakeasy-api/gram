import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
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

function application(
  overrides: Partial<IdentityProviderApplication> = {},
): IdentityProviderApplication {
  return {
    sourceApplicationId: "0oaexampleapp1",
    label: "Example Chat",
    providerStatus: "ACTIVE",
    signOnUrl: "https://chat.example.test/sso",
    logoUrl: "https://cdn.example.test/example-chat.png",
    groupAssignmentCount: 3,
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

  it("lists what Okta has, with the counts it could read", () => {
    withApplications([
      application(),
      application({
        sourceApplicationId: "0oaexampleapp2",
        label: "Example Docs",
        providerStatus: "INACTIVE",
        signOnUrl: "https://docs.example.test/acs",
        logoUrl: undefined,
        groupAssignmentCount: 1,
        userAssignmentCount: 1,
      }),
    ]);
    const { container } = render(
      <OktaApplicationsSection index={4} connection={connection()} />,
    );

    expect(screen.getByText("Example Chat")).toBeTruthy();
    // The host is what tells two similarly named applications apart.
    expect(screen.getByText("chat.example.test")).toBeTruthy();
    expect(screen.getByText("3 groups, 12 people")).toBeTruthy();
    // One of each reads as one of each, not "1 groups, 1 people".
    expect(screen.getByText("1 group, 1 person")).toBeTruthy();
    expect(screen.getByText(/2 applications read from Okta at/)).toBeTruthy();
    const logo = container.querySelector(
      'img[src="https://cdn.example.test/example-chat.png"]',
    );
    expect(logo).toBeTruthy();
    fireEvent.error(logo!);
    expect(screen.getByText("EC")).toBeTruthy();
  });

  it("says a dash where Okta did not give a count, and why", () => {
    withApplications(
      [
        application({
          groupAssignmentCount: undefined,
          userAssignmentCount: undefined,
        }),
      ],
      "Assignment counts were omitted because the tenant has more than 50 applications.",
    );
    render(<OktaApplicationsSection index={4} connection={connection()} />);

    // A dash on its own would read as "none"; the server's sentence is what
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

  it("narrows the list by name without asking Okta again", async () => {
    const refetch = vi.fn();
    withApplications([
      application(),
      application({
        sourceApplicationId: "0oaexampleapp2",
        label: "Example Docs",
      }),
    ]);
    applications.current.refetch = refetch;
    render(<OktaApplicationsSection index={4} connection={connection()} />);

    fireEvent.change(screen.getByPlaceholderText("Search applications"), {
      target: { value: "docs" },
    });

    // The box debounces, so the list narrows a moment after typing.
    await waitFor(() => expect(screen.queryByText("Example Chat")).toBeNull());
    expect(screen.getByText("Example Docs")).toBeTruthy();
    // The whole tenant is already in hand, so filtering is local.
    expect(refetch).not.toHaveBeenCalled();
  });

  it("offers a refresh that re-reads Okta", () => {
    const refetch = vi.fn();
    withApplications([application()]);
    applications.current.refetch = refetch;
    render(<OktaApplicationsSection index={4} connection={connection()} />);

    fireEvent.click(screen.getByRole("button", { name: /refresh/i }));
    expect(refetch).toHaveBeenCalledOnce();
  });

  it("changes nothing: no selection, no proposal, no matching", () => {
    withApplications([application()]);
    const { container } = render(
      <OktaApplicationsSection index={4} connection={connection()} />,
    );

    expect(container.querySelectorAll("input[type=checkbox]")).toHaveLength(0);
    expect(screen.queryByText(/MCP server/i)).toBeNull();
    expect(screen.queryByRole("button", { name: /apply/i })).toBeNull();
  });
});
