import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { IdentityProviderConnection } from "@gram/client/models/components/identityproviderconnection.js";
import type { IdentityProviderSetupStep } from "@gram/client/models/components/identityprovidersetupstep.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { OktaConnectSection } from "./okta-connect-section";

const setup = vi.hoisted(() => ({
  current: { data: undefined, isPending: false } as {
    data:
      | { connectionId: string; steps: IdentityProviderSetupStep[] }
      | undefined;
    isPending: boolean;
  },
}));
const create = vi.hoisted(() => ({
  mutate: vi.fn(),
  isPending: false,
  error: null,
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
const remove = vi.hoisted(() => ({ mutate: vi.fn(), isPending: false }));

vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
vi.mock("@gram/client/react-query/identityProvider.js", () => ({
  invalidateAllIdentityProvider: vi.fn(),
}));
vi.mock("@gram/client/react-query/identityProviderSetup.js", () => ({
  useIdentityProviderSetup: () => setup.current,
  invalidateAllIdentityProviderSetup: vi.fn(),
}));
vi.mock("@gram/client/react-query/createIdentityProvider.js", () => ({
  useCreateIdentityProviderMutation: () => create,
}));
vi.mock("@gram/client/react-query/submitIdentityProviderSetupStep.js", () => ({
  useSubmitIdentityProviderSetupStepMutation: () => submit,
}));
vi.mock("@gram/client/react-query/verifyIdentityProviderSetupStep.js", () => ({
  useVerifyIdentityProviderSetupStepMutation: () => verify,
}));
vi.mock("@gram/client/react-query/deleteIdentityProvider.js", () => ({
  useDeleteIdentityProviderMutation: () => remove,
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
    status: "pending",
    capabilities: [],
    grantedScopes: [],
    jwksUrl: `https://app.example.test/.well-known/identity-provider/abc/jwks.json`,
    signingKeyKid: "kid-abc123",
    createdAt: new Date("2026-09-14T10:00:00Z"),
    updatedAt: new Date("2026-09-14T10:00:00Z"),
    ...overrides,
  };
}

function connectStep(
  overrides: Partial<IdentityProviderSetupStep> = {},
): IdentityProviderSetupStep {
  return {
    key: "connect",
    title: "Install Speakeasy's key in Okta",
    where: "their_console",
    instructions: ["Create an API Services app integration."],
    printedValues: [
      {
        label: "Speakeasy's public keys",
        value:
          "https://app.example.test/.well-known/identity-provider/abc/jwks.json",
        copyable: true,
      },
    ],
    expectedValues: [{ key: "client_id", label: "Client ID", secret: false }],
    state: "awaiting_values",
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
  create.mutate.mockReset();
  submit.mutate.mockReset();
  verify.mutate.mockReset();
  remove.mutate.mockReset();
  verify.error = null;
});

function renderSection(conn?: IdentityProviderConnection) {
  return render(
    <OktaConnectSection connection={conn} isLoadingConnection={false} />,
  );
}

describe("OktaConnectSection", () => {
  it("asks for the organization URL before anything else", () => {
    renderSection(undefined);

    expect(screen.getByLabelText("Okta organization URL")).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Remove connection" }),
    ).toBeNull();

    fireEvent.change(screen.getByLabelText("Okta organization URL"), {
      target: { value: `https://${TENANT}` },
    });
    fireEvent.click(screen.getByRole("button", { name: "Connect Okta" }));

    expect(create.mutate).toHaveBeenCalledOnce();
    expect(create.mutate.mock.calls[0]![0]).toEqual({
      request: {
        createRequestBody2: { kind: "okta", tenantUrl: `https://${TENANT}` },
      },
    });
  });

  it("prints what Speakeasy publishes and asks for the value that comes back", () => {
    withStep(connectStep());
    renderSection(connection());

    expect(screen.getByText("Speakeasy's public keys")).toBeTruthy();
    expect(
      screen.getByText(
        "https://app.example.test/.well-known/identity-provider/abc/jwks.json",
      ),
    ).toBeTruthy();
    expect(screen.getByLabelText("Client ID")).toBeTruthy();
    // The escape hatch is removal once a connection exists; switching provider
    // is the grid's job, and it refuses while a connection is live.
    expect(
      screen.getByRole("button", { name: "Remove connection" }),
    ).toBeTruthy();

    fireEvent.change(screen.getByLabelText("Client ID"), {
      target: { value: "0oaexampleclientid" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(submit.mutate.mock.calls[0]![0]).toEqual({
      request: {
        submitSetupStepRequestBody: {
          stepKey: "connect",
          values: [{ key: "client_id", value: "0oaexampleclientid" }],
        },
      },
    });
  });

  it("shows the value already submitted, and saving it again is an update", () => {
    withStep(
      connectStep({
        expectedValues: [
          {
            key: "client_id",
            label: "Client ID",
            secret: false,
            currentValue: "0oaexampleclientid",
          },
        ],
      }),
    );
    renderSection(connection());

    const field = screen.getByLabelText("Client ID") as HTMLInputElement;
    expect(field.value).toBe("0oaexampleclientid");
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();

    // Correcting it sends what is on screen now, not what came back.
    fireEvent.change(field, { target: { value: "0oacorrectedvalue" } });
    fireEvent.click(screen.getByRole("button", { name: "Update" }));
    expect(submit.mutate.mock.calls[0]![0]).toEqual({
      request: {
        submitSetupStepRequestBody: {
          stepKey: "connect",
          values: [{ key: "client_id", value: "0oacorrectedvalue" }],
        },
      },
    });
  });

  it("never prefills a secret, because the server does not hand one back", () => {
    withStep(
      connectStep({
        expectedValues: [
          { key: "client_secret", label: "Client secret", secret: true },
        ],
      }),
    );
    renderSection(connection());

    const field = screen.getByLabelText("Client secret") as HTMLInputElement;
    expect(field.value).toBe("");
    expect(screen.getByRole("button", { name: "Save" })).toBeTruthy();
  });

  it("offers the check once the value is in", () => {
    withStep(connectStep({ state: "awaiting_verification" }));
    renderSection(connection({ status: "awaiting_verification" }));

    fireEvent.click(screen.getByRole("button", { name: "Verify connection" }));
    expect(verify.mutate.mock.calls[0]![0]).toEqual({
      request: { verifySetupStepRequestBody: { stepKey: "connect" } },
    });
  });

  it("still offers the check after one has failed", () => {
    // The server takes another check from a failed step, so hiding the button
    // here would strand the administrator on a fixable error.
    withStep(connectStep({ state: "failed" }));
    renderSection(connection({ status: "failed" }));

    fireEvent.click(screen.getByRole("button", { name: "Verify connection" }));
    expect(verify.mutate).toHaveBeenCalledOnce();
  });

  it("says so plainly while the check cannot run at all", () => {
    // The backend answers 503 until its verification lands; the step has to
    // say that rather than read as a failed check. The SDK decodes a typed
    // body only for 4XX, 500 and 502, so a 503 is a bare GramError.
    const unavailable = Object.create(GramError.prototype) as GramError;
    Object.assign(unavailable, { statusCode: 503, message: "unavailable" });
    verify.error = unavailable;

    withStep(connectStep({ state: "awaiting_verification" }));
    renderSection(connection({ status: "awaiting_verification" }));

    expect(screen.getByText("Verification is not available yet")).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Verify connection" }),
    ).toBeTruthy();
  });

  it("shows the connection and its key once it has proved out", () => {
    renderSection(
      connection({
        status: "active",
        clientId: "0oaexampleclientid",
        capabilities: ["directory_read"],
        lastVerifiedAt: new Date("2026-09-14T12:30:00Z"),
        verifyEvidence: {
          checkedAt: new Date("2026-09-14T12:30:00Z"),
          reads: [
            {
              capability: "directory_read",
              resource: "groups",
              ok: true,
              count: 18,
            },
          ],
        },
      }),
    );

    expect(screen.getByText(TENANT)).toBeTruthy();
    expect(screen.getByText("Connected")).toBeTruthy();
    expect(screen.getByText("kid-abc123")).toBeTruthy();
    expect(screen.getByText("0oaexampleclientid")).toBeTruthy();
    expect(screen.getByText("People and group membership")).toBeTruthy();
    expect(screen.getByText("18 groups")).toBeTruthy();
    // The exchange gives way to the connection once it has worked.
    expect(screen.queryByLabelText("Client ID")).toBeNull();
  });
});
