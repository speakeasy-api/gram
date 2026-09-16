import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { IdentityProviderConnection } from "@gram/client/models/components/identityproviderconnection.js";
import type { IdentityProviderSetupStep } from "@gram/client/models/components/identityprovidersetupstep.js";
import { OktaDirectorySection } from "./okta-directory-section";

const setup = vi.hoisted(() => ({
  current: { data: undefined, isPending: false } as {
    data:
      | { connectionId: string; steps: IdentityProviderSetupStep[] }
      | undefined;
    isPending: boolean;
  },
}));
const submit = vi.hoisted(() => ({
  mutate: vi.fn(),
  isPending: false,
  error: null,
}));
const verify = vi.hoisted(() => ({
  mutate: vi.fn(),
  isPending: false,
  error: null as unknown,
  data: undefined,
}));
const portal = vi.hoisted(() => ({ mutate: vi.fn(), isPending: false }));

vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
vi.mock("@gram/client/react-query/identityProvider.js", () => ({
  invalidateAllIdentityProvider: vi.fn(),
}));
vi.mock("@gram/client/react-query/identityProviderSetup.js", () => ({
  useIdentityProviderSetup: () => setup.current,
  invalidateAllIdentityProviderSetup: vi.fn(),
}));
vi.mock("@gram/client/react-query/submitIdentityProviderSetupStep.js", () => ({
  useSubmitIdentityProviderSetupStepMutation: () => submit,
}));
vi.mock("@gram/client/react-query/verifyIdentityProviderSetupStep.js", () => ({
  useVerifyIdentityProviderSetupStepMutation: () => verify,
}));
vi.mock("@gram/client/react-query/generateWorkOSAdminPortalLink.js", () => ({
  useGenerateWorkOSAdminPortalLinkMutation: () => portal,
}));

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

function directoryStep(
  overrides: Partial<IdentityProviderSetupStep> = {},
): IdentityProviderSetupStep {
  return {
    key: "directory",
    title: "Directory sync",
    where: "our_page",
    instructions: ["Speakeasy creates the directory application in Okta."],
    printedValues: [],
    expectedValues: [],
    state: "not_started",
    ...overrides,
  };
}

function withStep(step: IdentityProviderSetupStep) {
  setup.current = {
    data: { connectionId: "conn-1", steps: [step] },
    isPending: false,
  };
}

afterEach(cleanup);
beforeEach(() => {
  setup.current = { data: undefined, isPending: false };
  submit.mutate.mockReset();
  verify.mutate.mockReset();
  portal.mutate.mockReset();
  verify.error = null;
  verify.data = undefined;
});

describe("OktaDirectorySection", () => {
  it("creates the directory application with an empty submission", () => {
    withStep(directoryStep());
    render(<OktaDirectorySection index={3} connection={connection()} />);

    fireEvent.click(
      screen.getByRole("button", { name: "Set up directory sync" }),
    );

    expect(submit.mutate.mock.calls[0]![0]).toEqual({
      request: {
        submitSetupStepRequestBody: { stepKey: "directory", values: [] },
      },
    });
  });

  it("conceals the token until it is explicitly revealed", () => {
    withStep(
      directoryStep({
        state: "awaiting_verification",
        deepLink:
          "https://example-admin.okta.com/admin/app/scim2testapp/instance/app-1/#tab-provisioning",
        printedValues: [
          {
            label: "SCIM base URL",
            value: "https://api.example.test/scim/v2",
            copyable: true,
            secret: false,
          },
          {
            label: "Bearer token",
            value: "secret-token-value",
            copyable: true,
            secret: true,
          },
        ],
      }),
    );
    render(
      <OktaDirectorySection
        index={3}
        connection={connection({ directoryState: "configured" })}
      />,
    );

    expect(screen.getByText("https://api.example.test/scim/v2")).toBeTruthy();
    expect(screen.queryByText("secret-token-value")).toBeNull();
    expect(
      screen.getByRole("button", {
        name: "Open the Provisioning tab in Okta",
      }),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Reveal" }));
    expect(screen.getByText("secret-token-value")).toBeTruthy();

    fireEvent.click(
      screen.getByRole("button", { name: "Check the directory" }),
    );
    expect(verify.mutate.mock.calls[0]![0]).toEqual({
      request: { verifySetupStepRequestBody: { stepKey: "directory" } },
    });
  });

  it("says the check is waiting, in the server's words", () => {
    withStep(
      directoryStep({
        state: "awaiting_verification",
        lastOutcome: {
          outcome: "pending_validation",
          detail: "No groups have reached Speakeasy yet.",
          capabilities: [],
          grantedScopes: [],
          evidence: {
            checkedAt: new Date("2026-09-15T12:00:00Z"),
            reads: [],
          },
        },
      }),
    );
    render(
      <OktaDirectorySection
        index={3}
        connection={connection({ directoryState: "pending_validation" })}
      />,
    );

    // One line: what happened, and the server's own sentence for why.
    expect(screen.getByText("Waiting")).toBeTruthy();
    expect(
      screen.getByText("No groups have reached Speakeasy yet."),
    ).toBeTruthy();
  });

  // Once the directory application exists the step asks for nothing, so it
  // offers nothing to send: checking is all that is left here.
  it("offers no submit once the application exists", () => {
    withStep(directoryStep({ state: "awaiting_verification" }));
    render(
      <OktaDirectorySection
        index={3}
        connection={connection({ directoryState: "configured" })}
      />,
    );

    expect(
      screen.queryByRole("button", { name: "Set up directory sync" }),
    ).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Assign new groups" }),
    ).toBeNull();
    expect(
      screen.getByRole("button", { name: "Check the directory" }),
    ).toBeTruthy();
  });

  it("reports the outcome as one line, with no evidence table", () => {
    withStep(
      directoryStep({
        state: "passed",
        lastOutcome: {
          outcome: "passed",
          detail: "Directory sync is running.",
          capabilities: ["group_assignment", "directory_read"],
          grantedScopes: [],
          evidence: {
            checkedAt: new Date("2026-09-15T12:00:00Z"),
            reads: [
              {
                capability: "group_assignment",
                resource: "directory_application",
                ok: true,
              },
              {
                capability: "group_assignment",
                resource: "directory_connection",
                ok: true,
              },
              {
                capability: "directory_read",
                resource: "directory_groups",
                ok: true,
                count: 4,
              },
              {
                capability: "directory_read",
                resource: "directory_users",
                ok: true,
                count: 12,
              },
            ],
          },
        },
      }),
    );
    render(
      <OktaDirectorySection
        index={3}
        connection={connection({ directoryState: "passed" })}
      />,
    );

    expect(screen.getByText("Passed")).toBeTruthy();
    expect(screen.getByText("Directory sync is running.")).toBeTruthy();

    // The rows are the same four reads whatever the outcome, so they tell the
    // reader nothing the sentence has not already said and nothing they can
    // act on. The connect and sign-on steps keep theirs.
    expect(screen.queryByText("Directory application in Okta")).toBeNull();
    expect(screen.queryByText("Provisioning connection in Okta")).toBeNull();
    expect(screen.queryByText("Groups arrived from Okta")).toBeNull();
    expect(screen.queryByText("4 groups")).toBeNull();
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("submits the directory step even when sign-on has failed", () => {
    withStep(directoryStep());
    render(
      <OktaDirectorySection
        index={3}
        connection={connection({ signInState: "failed" })}
      />,
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Set up directory sync" }),
    );

    expect(submit.mutate.mock.calls[0]![0]).toEqual({
      request: {
        submitSetupStepRequestBody: { stepKey: "directory", values: [] },
      },
    });
  });

  it("uses the WorkOS portal when the server returns a dsync handoff", () => {
    withStep(directoryStep({ portalIntent: "dsync" }));
    render(<OktaDirectorySection index={3} connection={connection()} />);

    fireEvent.click(screen.getByRole("button", { name: "Connect" }));
    expect(portal.mutate.mock.calls[0]![0]).toEqual({
      request: {
        generateWorkOSAdminPortalLinkRequestBody: {
          intent: "dsync",
          successUrl: expect.stringContaining("intent=dsync"),
          returnUrl: window.location.href,
        },
      },
    });
  });
});
