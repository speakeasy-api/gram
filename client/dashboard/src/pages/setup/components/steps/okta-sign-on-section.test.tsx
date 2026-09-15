import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { IdentityProviderConnection } from "@gram/client/models/components/identityproviderconnection.js";
import type { IdentityProviderSetupStep } from "@gram/client/models/components/identityprovidersetupstep.js";
import { OktaSignOnSection } from "./okta-sign-on-section";

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

// Placeholder tenant only — never a real customer's Okta hostname.
const TENANT = "example.okta.com";

function connection(
  overrides: Partial<IdentityProviderConnection> = {},
): IdentityProviderConnection {
  return {
    id: "conn-1",
    kind: "okta",
    tenantIdentifier: TENANT,
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

function signInStep(
  overrides: Partial<IdentityProviderSetupStep> = {},
): IdentityProviderSetupStep {
  return {
    key: "sign_in",
    title: "Sign-in application",
    where: "our_page",
    instructions: ["Speakeasy creates the application in Okta for you."],
    printedValues: [],
    expectedValues: [],
    state: "not_started",
    claims: [
      {
        name: "email",
        purpose: "Identity",
        carriesAccess: false,
        provisioned: false,
      },
      {
        name: "groups",
        purpose: "Roles",
        carriesAccess: true,
        provisioned: true,
      },
    ],
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
});

describe("OktaSignOnSection", () => {
  it("waits until the connection itself is live", () => {
    render(
      <OktaSignOnSection
        index={2}
        connection={connection({ status: "pending" })}
      />,
    );

    const section = screen.getByRole("region", { hidden: true });
    expect(section.getAttribute("aria-disabled")).toBe("true");
    expect(section.querySelectorAll("button")).toHaveLength(0);
    expect(screen.getByText("Waiting")).toBeTruthy();
  });

  it("offers to create the application, and says what sign-in will carry", () => {
    withStep(signInStep());
    render(<OktaSignOnSection index={2} connection={connection()} />);

    // The claim table is configuration, not an observation.
    expect(screen.getByText("email")).toBeTruthy();
    expect(screen.getByText("groups")).toBeTruthy();
    expect(screen.getByText("Carries access")).toBeTruthy();
    expect(screen.getByText("Provisioned")).toBeTruthy();

    fireEvent.click(
      screen.getByRole("button", { name: "Create the sign-in application" }),
    );
    expect(submit.mutate.mock.calls[0]![0]).toEqual({
      request: {
        submitSetupStepRequestBody: { stepKey: "sign_in", values: [] },
      },
    });
  });

  it("hands the secret to the portal once the application exists", () => {
    withStep(
      signInStep({
        state: "awaiting_verification",
        portalIntent: "sso",
        deepLink: "https://example-admin.okta.com/admin/app/oidc_client/abc",
        printedValues: [
          { label: "Client ID", value: "0oaexampleclientid", copyable: true },
        ],
      }),
    );
    render(
      <OktaSignOnSection
        index={2}
        connection={connection({ signInState: "application_created" })}
      />,
    );

    expect(
      screen.getByText("The sign-in application was created in Okta."),
    ).toBeTruthy();
    expect(screen.getByText("0oaexampleclientid")).toBeTruthy();
    // Speakeasy has nowhere to put the secret, so it never asks for one.
    expect(screen.queryByLabelText(/secret/i)).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Connect" }));
    expect(portal.mutate).toHaveBeenCalledOnce();

    fireEvent.click(
      screen.getByRole("button", { name: "Check the connection" }),
    );
    expect(verify.mutate.mock.calls[0]![0]).toEqual({
      request: { verifySetupStepRequestBody: { stepKey: "sign_in" } },
    });
  });

  it("guides the groups claim and records whichever way it is settled", () => {
    const repair = {
      title: "Okta is not sending group membership yet",
      instructions: ["Open the Sign On tab.", "Set the groups claim filter."],
      deepLink: "https://example-admin.okta.com/admin/app/oidc_client/abc",
      fallbackAvailable: true,
    };
    withStep(signInStep({ state: "awaiting_verification", repair }));
    render(
      <OktaSignOnSection
        index={2}
        connection={connection({ signInState: "application_created" })}
      />,
    );

    expect(screen.getByText(repair.title)).toBeTruthy();
    expect(screen.getByText("Set the groups claim filter.")).toBeTruthy();

    fireEvent.click(
      screen.getByRole("button", { name: "I have set the groups claim" }),
    );
    expect(submit.mutate.mock.calls[0]![0]).toEqual({
      request: {
        submitSetupStepRequestBody: {
          stepKey: "sign_in",
          values: [{ key: "groups_claim_confirmed", value: "true" }],
        },
      },
    });

    submit.mutate.mockReset();
    fireEvent.click(
      screen.getByRole("button", { name: "Use directory groups instead" }),
    );
    expect(submit.mutate.mock.calls[0]![0]).toEqual({
      request: {
        submitSetupStepRequestBody: {
          stepKey: "sign_in",
          values: [{ key: "groups_source", value: "directory" }],
        },
      },
    });
  });

  it("replaces the groups actions with the choice already made", () => {
    withStep(
      signInStep({
        state: "awaiting_verification",
        repair: {
          title: "Okta is not sending group membership yet",
          instructions: ["Set the groups claim filter."],
          fallbackAvailable: true,
        },
      }),
    );
    render(
      <OktaSignOnSection
        index={2}
        connection={connection({
          signInState: "application_created",
          groupsSource: "directory",
        })}
      />,
    );

    expect(
      screen.getByText(/Group membership is read from the directory/),
    ).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "I have set the groups claim" }),
    ).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Use directory groups instead" }),
    ).toBeNull();
  });

  it("says sign-in is waiting for its first sign-in, without calling it done", () => {
    withStep(
      signInStep({
        state: "awaiting_verification",
        lastOutcome: {
          outcome: "pending_validation",
          detail:
            "Speakeasy has configured sign-in. It becomes active after the first successful sign-in through Okta.",
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
      <OktaSignOnSection
        index={2}
        connection={connection({ signInState: "application_created" })}
      />,
    );

    expect(
      screen.getByText(
        "Sign-in is configured and waiting for its first sign-in",
      ),
    ).toBeTruthy();
    // The server says what happens next better than we can, so its own
    // sentence is the body rather than a footnote under ours.
    expect(
      screen.getByText(/becomes active after the first successful/),
    ).toBeTruthy();
    // Waiting is not failing: the check stays available and the step is open.
    expect(
      screen.getByRole("button", { name: "Check the connection" }),
    ).toBeTruthy();
    const section = screen.getByRole("region", { hidden: true });
    expect(section.querySelector(".lucide-check")).toBeNull();
  });

  it("shows both reads once the check passes, and completes the step", () => {
    withStep(
      signInStep({
        state: "passed",
        lastOutcome: {
          outcome: "passed",
          detail: "Sign-on is configured.",
          capabilities: ["sign_in"],
          grantedScopes: [],
          evidence: {
            checkedAt: new Date("2026-09-15T12:00:00Z"),
            reads: [
              {
                capability: "sign_in",
                resource: "sign_in_application",
                ok: true,
                detail: "Okta sign-in application is active.",
              },
              {
                capability: "sign_in",
                resource: "sign_in_connection",
                ok: true,
                detail: "Sign-in provider connection is active.",
              },
            ],
          },
        },
      }),
    );
    render(
      <OktaSignOnSection
        index={2}
        connection={connection({ signInState: "passed" })}
      />,
    );

    // Two reads, one per side of the same capability.
    expect(screen.getByText("Sign-in application in Okta")).toBeTruthy();
    expect(screen.getByText("Speakeasy's sign-in provider")).toBeTruthy();
    expect(
      screen.getByText("Okta sign-in application is active."),
    ).toBeTruthy();
    // The step's own tick: a check mark replaces its number.
    const section = screen.getByRole("region", { hidden: true });
    expect(section.querySelector(".lucide-check")).toBeTruthy();
  });
});
