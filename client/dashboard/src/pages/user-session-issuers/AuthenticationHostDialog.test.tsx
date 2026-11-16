import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthenticationHostDialog } from "./AuthenticationHostDialog";

const testState = vi.hoisted(() => ({
  mutate: vi.fn(),
  impact: {
    data: undefined as
      | {
          authorizationServerBefore: string;
          authorizationServerAfter: string;
          changes: boolean;
          mcpServerCount: number;
        }
      | undefined,
    isFetching: false,
    isError: false,
  },
  impactRequests: [] as unknown[],
}));

vi.mock(
  "@gram/client/react-query/organizationUserSessionIssuerAuthenticationHostImpact.js",
  () => ({
    useOrganizationUserSessionIssuerAuthenticationHostImpact: (
      request: unknown,
    ) => {
      testState.impactRequests.push(request);
      return testState.impact;
    },
  }),
);

vi.mock(
  "@gram/client/react-query/setOrganizationUserSessionIssuerAuthenticationHost.js",
  () => ({
    useSetOrganizationUserSessionIssuerAuthenticationHostMutation: () => ({
      mutate: testState.mutate,
      isPending: false,
      isError: false,
    }),
  }),
);

vi.mock("./invalidateIssuers", () => ({ invalidateIssuers: vi.fn() }));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn() } }));

function issuer(overrides: Partial<UserSessionIssuer>): UserSessionIssuer {
  return {
    id: "issuer-1",
    slug: "workforce",
    projectId: "",
    authorizationServerMode: "shared",
    useAuthenticationHost: false,
    ...overrides,
  } as UserSessionIssuer;
}

beforeEach(() => {
  testState.mutate.mockReset();
  testState.impact = { data: undefined, isFetching: false, isError: false };
  testState.impactRequests = [];
});

afterEach(() => {
  cleanup();
});

describe("AuthenticationHostDialog", () => {
  it("names the address clients move to before moving", () => {
    testState.impact.data = {
      authorizationServerBefore: "https://app.example.com/oauth/usi/issuer-1",
      authorizationServerAfter: "https://id.example.com/oauth/usi/issuer-1",
      changes: true,
      mcpServerCount: 2,
    };
    render(<AuthenticationHostDialog issuer={issuer({})} onClose={() => {}} />);

    expect(
      screen.getByText("Move workforce to the authentication host?"),
    ).toBeDefined();
    expect(
      screen.getByText(
        /sign in at https:\/\/id\.example\.com\/oauth\/usi\/issuer-1 instead of https:\/\/app\.example\.com/,
      ),
    ).toBeDefined();
    expect(testState.impactRequests).toContainEqual({
      id: "issuer-1",
      useAuthenticationHost: true,
    });

    fireEvent.click(screen.getByRole("button", { name: "Move" }));
    expect(testState.mutate).toHaveBeenCalledWith({
      request: {
        setAuthenticationHostRequestBody: {
          id: "issuer-1",
          useAuthenticationHost: true,
        },
      },
    });
  });

  it("moves back off the authentication host", () => {
    testState.impact.data = {
      authorizationServerBefore: "https://id.example.com",
      authorizationServerAfter: "https://app.example.com",
      changes: true,
      mcpServerCount: 1,
    };
    render(
      <AuthenticationHostDialog
        issuer={issuer({
          authorizationServerMode: "endpoint",
          useAuthenticationHost: true,
        })}
        onClose={() => {}}
      />,
    );

    expect(
      screen.getByText("Move workforce off the authentication host?"),
    ).toBeDefined();
    expect(
      screen.getByText(/custom domain keep their own address/),
    ).toBeDefined();
  });

  it("does not offer a move that changes nothing", () => {
    testState.impact.data = {
      authorizationServerBefore: "https://app.example.com",
      authorizationServerAfter: "https://app.example.com",
      changes: false,
      mcpServerCount: 0,
    };
    render(<AuthenticationHostDialog issuer={issuer({})} onClose={() => {}} />);

    expect(
      screen.getByRole("button", { name: "Move" }).hasAttribute("disabled"),
    ).toBe(true);
  });
});
