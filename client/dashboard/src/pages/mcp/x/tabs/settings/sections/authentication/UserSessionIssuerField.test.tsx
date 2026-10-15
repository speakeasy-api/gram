import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AuthTarget } from "./authTarget";
import { UserSessionIssuerField } from "./UserSessionIssuerField";

const testState = vi.hoisted(() => ({
  impact: {
    data: undefined as
      | {
          authorizationServerChanges: boolean;
          candidateIssuer?: { issuerUrl?: string };
        }
      | undefined,
    isFetching: false,
    isError: false,
  },
  impactRequests: [] as unknown[],
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    children,
  }: {
    children: ReactNode | ((props: { disabled: boolean }) => ReactNode);
  }) => (
    <>
      {typeof children === "function"
        ? children({ disabled: false })
        : children}
    </>
  ),
}));

// The real select is a Radix popover; a native one keeps the test on the
// field's save flow.
vi.mock("@/components/user-session-issuer-select", () => ({
  UserSessionIssuerSelect: ({
    issuers,
    value,
    onValueChange,
  }: {
    issuers: UserSessionIssuer[];
    value: string;
    onValueChange: (value: string) => void;
  }) => (
    <select
      aria-label="User session issuer"
      value={value}
      onChange={(event) => onValueChange(event.target.value)}
    >
      <option value="">None</option>
      {issuers.map((issuer) => (
        <option key={issuer.id} value={issuer.id}>
          {issuer.slug}
        </option>
      ))}
    </select>
  ),
}));

vi.mock("@/lib/remote-identity", () => ({
  ScopeBadge: () => null,
}));

vi.mock("@gram/client/react-query/userSessionIssuerAttachImpact.js", () => ({
  useUserSessionIssuerAttachImpact: (request: unknown) => {
    testState.impactRequests.push(request);
    return testState.impact;
  },
}));

vi.mock("@gram/client/react-query/userSessionIssuer.js", () => ({
  invalidateAllUserSessionIssuer: vi.fn(),
}));

vi.mock("@gram/client/react-query/userSessionIssuers.js", () => ({
  invalidateAllUserSessionIssuers: vi.fn(),
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

function issuer(id: string): UserSessionIssuer {
  return {
    id,
    slug: id,
    authnChallengeMode: "interactive",
    authorizationServerMode: "endpoint",
    clientIdMetadataAdmissionMode: "open",
    sessionDurationHours: 24,
    useAuthenticationHost: false,
    issuerUrlPinned: false,
    organizationId: "org-1",
    projectId: "project-1",
    createdAt: new Date(0),
    updatedAt: new Date(0),
  };
}

const link = vi.fn<(userSessionIssuerId: string) => Promise<void>>();

function renderField(currentIssuerId: string | null): void {
  const target: AuthTarget = {
    kind: "standard",
    slug: "server",
    projectId: "project-1",
    permissionResourceId: "toolset-1",
    attachImpactTarget: { toolsetId: "toolset-1" },
    supportsOrganizationIssuers: true,
    userSessionIssuerId: currentIssuerId,
    linkUserSessionIssuer: link,
    invalidate: vi.fn(async () => {}),
  };
  render(
    <QueryClientProvider client={new QueryClient()}>
      <UserSessionIssuerField
        target={target}
        issuers={[issuer("current"), issuer("candidate")]}
        isLoading={false}
        isError={false}
      />
    </QueryClientProvider>,
  );
}

function chooseCandidateAndSave(): void {
  fireEvent.change(screen.getByLabelText("User session issuer"), {
    target: { value: "candidate" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
}

beforeEach(() => {
  link.mockReset();
  link.mockResolvedValue(undefined);
  testState.impact = { data: undefined, isFetching: false, isError: false };
  testState.impactRequests = [];
});

afterEach(() => {
  cleanup();
});

describe("UserSessionIssuerField", () => {
  it("confirms before replacing an issuer and names the new authorization server", async () => {
    testState.impact.data = {
      authorizationServerChanges: true,
      candidateIssuer: { issuerUrl: "https://app.example.com/oauth/usi/x" },
    };
    renderField("current");

    chooseCandidateAndSave();

    expect(screen.getByText("Change user session issuer?")).toBeDefined();
    expect(link).not.toHaveBeenCalled();
    expect(testState.impactRequests).toContainEqual({
      toolsetId: "toolset-1",
      userSessionIssuerId: "candidate",
    });
    expect(
      screen.getByText(
        /different authorization server, https:\/\/app\.example\.com\/oauth\/usi\/x/,
      ),
    ).toBeDefined();

    fireEvent.click(screen.getByRole("button", { name: "Change issuer" }));
    await waitFor(() => expect(link).toHaveBeenCalledWith("candidate"));
  });

  it("says when clients keep the same authorization server", () => {
    testState.impact.data = { authorizationServerChanges: false };
    renderField("current");

    chooseCandidateAndSave();

    expect(
      screen.getByText("Clients keep using the same authorization server."),
    ).toBeDefined();
  });

  it("still lets the change through when the impact check fails", async () => {
    testState.impact.isError = true;
    renderField("current");

    chooseCandidateAndSave();

    expect(screen.getByText(/Couldn't check whether/)).toBeDefined();
    fireEvent.click(screen.getByRole("button", { name: "Change issuer" }));
    await waitFor(() => expect(link).toHaveBeenCalledWith("candidate"));
  });

  it("confirms before a first issuer starts requiring sign-in", async () => {
    testState.impact.data = {
      authorizationServerChanges: true,
      candidateIssuer: { issuerUrl: "https://app.example.com/oauth/usi/x" },
    };
    renderField(null);

    chooseCandidateAndSave();

    expect(screen.getByText("Require sign-in for this server?")).toBeDefined();
    expect(
      screen.getByText(
        "Clients will sign in through the shared authorization server, https://app.example.com/oauth/usi/x.",
      ),
    ).toBeDefined();
    expect(link).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Require sign-in" }));
    await waitFor(() => expect(link).toHaveBeenCalledWith("candidate"));
  });

  it("waits for a refreshed preview before confirming", () => {
    testState.impact.data = { authorizationServerChanges: false };
    testState.impact.isFetching = true;
    renderField("current");

    chooseCandidateAndSave();

    expect(
      screen
        .getByRole("button", { name: "Change issuer" })
        .hasAttribute("disabled"),
    ).toBe(true);
  });
});
