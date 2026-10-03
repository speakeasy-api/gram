import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Assistant } from "@gram/client/models/components/assistant.js";
import { AssistantIdentitySettings } from "./AssistantIdentitySettings";
const mocks = vi.hoisted(() => ({ mutate: vi.fn(), canWrite: true }));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string) => scope === "project:write" && mocks.canWrite,
  }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    identities: {
      detail: { overview: { href: (id: string) => `/identities/${id}` } },
    },
  }),
}));
vi.mock("@gram/client/react-query/assistantsUpgradeIdentity.js", () => ({
  useAssistantsUpgradeIdentityMutation: () => ({
    mutate: mocks.mutate,
    isPending: false,
  }),
}));
const assistant: Assistant = {
  id: "11111111-1111-4111-8111-111111111111",
  projectId: "22222222-2222-4222-8222-222222222222",
  name: "Example assistant",
  model: "example/model",
  instructions: "",
  toolsets: [],
  mcpServers: [],
  skills: [],
  warmTtlSeconds: 300,
  maxConcurrency: 1,
  status: "active",
  createdAt: new Date(),
  updatedAt: new Date(),
  identityState: "NEVER_CONFIGURED",
  identityDiagnostics: {
    health: "legacy",
    provisioningEnabled: true,
    executionEnabled: true,
    slackDelegationEnabled: true,
    bindings: [],
    bindingsTruncated: false,
  },
};
function setup(value: Assistant = assistant) {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>
        <AssistantIdentitySettings assistant={value} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  mocks.canWrite = true;
});
describe("Assistant identity management", () => {
  it("requires exact-target confirmation and preserves shared context guidance", () => {
    setup();
    expect(
      screen.getByText(/Thread history and replies are shared/),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Upgrade identity" }));
    expect(mocks.mutate).not.toHaveBeenCalled();
    expect(screen.getByText(`Assistant: ${assistant.id}`)).toBeTruthy();
    expect(screen.getByText(`Project: ${assistant.projectId}`)).toBeTruthy();
    fireEvent.click(
      screen.getByRole("button", { name: "Confirm identity change" }),
    );
    expect(mocks.mutate).toHaveBeenCalledWith({
      request: { riskIDRequestBody: { id: assistant.id } },
    });
  });
  it("hides mutation without project write or when provisioning is stopped", () => {
    mocks.canWrite = false;
    setup();
    expect(
      screen.queryByRole("button", { name: "Upgrade identity" }),
    ).toBeNull();
    cleanup();
    mocks.canWrite = true;
    setup({
      ...assistant,
      identityDiagnostics: {
        ...assistant.identityDiagnostics!,
        provisioningEnabled: false,
      },
    });
    expect(
      screen.queryByRole("button", { name: "Upgrade identity" }),
    ).toBeNull();
  });
  it("does not offer to repair suspended or withdrawn authority", () => {
    for (const health of ["suspended", "unavailable"] as const) {
      cleanup();
      setup({
        ...assistant,
        identityState: "ACTIVE",
        identityDiagnostics: {
          ...assistant.identityDiagnostics!,
          health,
          bindings: [
            {
              triggerId: "root",
              triggerKind: "slack",
              triggerStatus: "active",
              generation: 1,
              state: "missing",
            },
          ],
        },
      });
      expect(
        screen.queryByRole("button", { name: "Repair missing bindings" }),
      ).toBeNull();
    }
  });
  it("offers a confirmed idempotent repair when diagnostics are truncated", () => {
    setup({
      ...assistant,
      identityState: "ACTIVE",
      identityDiagnostics: {
        ...assistant.identityDiagnostics!,
        health: "ready",
        bindingsTruncated: true,
        bindings: [
          {
            triggerId: "visible-root",
            triggerKind: "slack",
            triggerStatus: "active",
            generation: 1,
            state: "ready",
          },
        ],
      },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Repair missing bindings" }),
    );
    expect(mocks.mutate).not.toHaveBeenCalled();
    fireEvent.click(
      screen.getByRole("button", { name: "Confirm identity change" }),
    );
    expect(mocks.mutate).toHaveBeenCalledWith({
      request: { riskIDRequestBody: { id: assistant.id } },
    });
  });
  it.each([
    { canWrite: false, provisioningEnabled: true, health: "ready" },
    { canWrite: true, provisioningEnabled: false, health: "ready" },
    { canWrite: true, provisioningEnabled: true, health: "suspended" },
    { canWrite: true, provisioningEnabled: true, health: "unavailable" },
  ] as const)(
    "preserves repair gates for truncated diagnostics: %o",
    ({ canWrite, provisioningEnabled, health }) => {
      mocks.canWrite = canWrite;
      setup({
        ...assistant,
        identityState: "ACTIVE",
        identityDiagnostics: {
          ...assistant.identityDiagnostics!,
          health,
          provisioningEnabled,
          bindingsTruncated: true,
        },
      });
      expect(
        screen.queryByRole("button", { name: "Repair missing bindings" }),
      ).toBeNull();
    },
  );
  it("does not offer repair for complete diagnostics without missing roots", () => {
    setup({
      ...assistant,
      identityState: "ACTIVE",
      identityDiagnostics: {
        ...assistant.identityDiagnostics!,
        health: "ready",
      },
    });
    expect(
      screen.queryByRole("button", { name: "Repair missing bindings" }),
    ).toBeNull();
  });
  it("offers repair only for missing roots on a configured identity", () => {
    setup({
      ...assistant,
      identityState: "ACTIVE",
      identityDiagnostics: {
        ...assistant.identityDiagnostics!,
        health: "ready",
        bindings: [
          {
            triggerId: "root",
            triggerKind: "slack",
            triggerStatus: "active",
            generation: 0,
            state: "missing",
          },
        ],
      },
    });
    expect(
      screen.getByRole("button", { name: "Repair missing bindings" }),
    ).toBeTruthy();
  });
});
