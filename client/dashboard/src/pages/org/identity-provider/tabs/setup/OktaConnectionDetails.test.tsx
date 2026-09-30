import { afterEach, describe, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@/components/ui/Tooltip";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";

import {
  ConnectionFacts,
  ReplaceClientSecretButton,
} from "./OktaConnectionDetails";
import { makeChecklistItem, makeConnection } from "./testFixtures";

vi.mock("@/lib/dates", () => ({
  HumanizeDateTime: ({ date }: { date: Date }) => (
    <time>{date.toISOString()}</time>
  ),
}));

const replaceMutation = vi.hoisted(() => ({
  mutate: vi.fn(),
  reset: vi.fn(),
  isPending: false,
  error: undefined as Error | undefined,
  onSuccess: undefined as
    | undefined
    | ((updated: OktaIdentityProviderConnection) => void),
}));
vi.mock(
  "@gram/client/react-query/replaceIdentityProviderConnectionClientSecret.js",
  () => ({
    useReplaceIdentityProviderConnectionClientSecretMutation: (options: {
      onSuccess: typeof replaceMutation.onSuccess;
    }) => {
      replaceMutation.onSuccess = options.onSuccess;
      return replaceMutation;
    },
  }),
);

afterEach(() => {
  cleanup();
  replaceMutation.mutate.mockClear();
  replaceMutation.reset.mockClear();
  replaceMutation.isPending = false;
  replaceMutation.error = undefined;
});

const connection = makeConnection({ status: "degraded" });

function facts(value: OktaIdentityProviderConnection) {
  return (
    <TooltipProvider>
      <ConnectionFacts connection={value} />
    </TooltipProvider>
  );
}

describe("ConnectionFacts last verification", () => {
  it("does not report unrelated agent edits as degraded check times", () => {
    const { container, rerender } = render(facts(connection));
    expect(screen.getByText("Never;")).toBeTruthy();
    expect(screen.getByText("needs attention")).toBeTruthy();
    expect(container.querySelector("time")).toBeNull();

    rerender(
      facts({
        ...connection,
        agentId: "agent-id",
        updatedAt: new Date("2026-01-03T00:00:00Z"),
      }),
    );
    expect(screen.getByText("needs attention")).toBeTruthy();
    expect(container.querySelector("time")).toBeNull();
  });

  it("retains the last successful verification timestamp after degradation", () => {
    const lastVerifiedAt = new Date("2026-01-01T12:00:00Z");
    render(facts({ ...connection, lastVerifiedAt }));
    expect(screen.getByText(lastVerifiedAt.toISOString())).toBeTruthy();
    expect(screen.queryByText("needs attention")).toBeNull();
    expect(screen.queryByText(connection.updatedAt.toISOString())).toBeNull();
  });
});

describe("ConnectionFacts token protection", () => {
  it.each([
    ["pending", true, "Not checked yet"],
    ["verified", true, "Protected"],
    ["degraded", false, "Not protected"],
  ] as const)(
    "describes %s protection without implying an unchecked result",
    (status, dpopRequired, label) => {
      render(facts({ ...connection, status, dpopRequired }));
      expect(screen.getByText("Token protection (DPoP)")).toBeTruthy();
      expect(screen.getByText(label)).toBeTruthy();
      expect(screen.getByText("Public key URL (JWKS)")).toBeTruthy();
    },
  );

  it("leaves token protection unchecked when no token was observed", () => {
    render(
      facts({
        ...connection,
        dpopRequired: false,
        checklist: [makeChecklistItem("dpop", "connect")],
      }),
    );
    expect(screen.getByText("Not checked yet")).toBeTruthy();
    expect(screen.queryByText("Not protected")).toBeNull();
  });

  it("does not reuse old token protection after a failed recheck", () => {
    render(facts({ ...connection, lastError: "okta_unreachable" }));
    expect(screen.getByText("Not checked yet")).toBeTruthy();
    expect(screen.queryByText("Protected")).toBeNull();
  });
});

describe("ConnectionFacts key material", () => {
  it("shows the key URL and signing key for key-based connections", () => {
    render(
      facts({
        ...connection,
        activeKey: {
          id: "key-1",
          kid: "kid-1",
          activatedAt: new Date("2026-09-01T00:00:00Z"),
        },
      }),
    );
    expect(screen.getByText("Public key URL (JWKS)")).toBeTruthy();
    expect(screen.getByText("Active signing key ID")).toBeTruthy();
    expect(screen.getByText("kid-1")).toBeTruthy();
  });

  it("hides key facts for connections that use a client secret", () => {
    render(facts({ ...connection, listingMode: "oin", jwksUrl: undefined }));
    expect(screen.queryByText("Public key URL (JWKS)")).toBeNull();
    expect(screen.queryByText("Active signing key ID")).toBeNull();
  });
});

describe("ConnectionFacts for OIN connections", () => {
  it("hides the JWKS row and labels the listing", () => {
    render(facts({ ...connection, listingMode: "oin", jwksUrl: undefined }));
    expect(screen.queryByText("Public key URL (JWKS)")).toBeNull();
    expect(screen.getByText("Okta catalog (OIN)")).toBeTruthy();
  });
});

describe("ReplaceClientSecretButton", () => {
  const oin = makeConnection({ listingMode: "oin", jwksUrl: undefined });

  function renderButton() {
    return render(
      <QueryClientProvider client={new QueryClient()}>
        <ReplaceClientSecretButton connection={oin} />
      </QueryClientProvider>,
    );
  }

  it("submits the new secret from the dialog", () => {
    renderButton();
    fireEvent.click(
      screen.getByRole("button", { name: "Replace client secret" }),
    );
    const confirm = screen.getByRole("button", { name: "Replace and verify" });
    expect(confirm.hasAttribute("disabled")).toBe(true);
    const secret = screen.getByLabelText(
      "New client secret",
    ) as HTMLInputElement;
    expect(secret.type).toBe("password");
    fireEvent.keyDown(secret, { key: "Enter" });
    expect(replaceMutation.mutate).not.toHaveBeenCalled();
    fireEvent.change(secret, { target: { value: " new-secret " } });
    fireEvent.click(confirm);
    expect(replaceMutation.mutate).toHaveBeenCalledExactlyOnceWith({
      security: expect.anything(),
      request: {
        replaceIdentityProviderConnectionClientSecretRequestBody: {
          id: oin.id,
          clientSecret: "new-secret",
        },
      },
    });
  });

  it("hides backend error details and clears the secret after success", () => {
    const { rerender } = renderButton();
    fireEvent.click(
      screen.getByRole("button", { name: "Replace client secret" }),
    );
    const secret = screen.getByLabelText(
      "New client secret",
    ) as HTMLInputElement;
    fireEvent.change(secret, { target: { value: "DEMO_REPLACEMENT_SECRET" } });
    fireEvent.keyDown(secret, { key: "Enter" });
    expect(replaceMutation.mutate).toHaveBeenCalledTimes(1);
    replaceMutation.isPending = true;
    replaceMutation.error = new Error("DEMO_REPLACEMENT_SECRET");
    rerender(
      <QueryClientProvider client={new QueryClient()}>
        <ReplaceClientSecretButton connection={oin} />
      </QueryClientProvider>,
    );
    expect(secret.disabled).toBe(true);
    expect(screen.getByRole("alert").textContent).not.toContain(
      "DEMO_REPLACEMENT_SECRET",
    );
    fireEvent.keyDown(secret, { key: "Enter" });
    expect(replaceMutation.mutate).toHaveBeenCalledTimes(1);
    act(() =>
      replaceMutation.onSuccess?.(makeConnection({ status: "verified" })),
    );
    expect(replaceMutation.reset).toHaveBeenCalled();
    expect(screen.queryByLabelText("New client secret")).toBeNull();
  });
});
