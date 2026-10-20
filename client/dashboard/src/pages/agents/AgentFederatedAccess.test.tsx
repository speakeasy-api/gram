import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";
import { afterEach, expect, it, vi } from "vitest";
import { AgentFederatedAccess } from "./AgentFederatedAccess";

const mocks = vi.hoisted(() => ({
  admissions: [] as WorkloadAdmission[],
  isPending: false,
  isError: false,
  withdraw: vi.fn(),
  isWithdrawing: false,
}));

vi.mock("@gram/client/react-query/workloadIdentities.js", () => ({
  useWorkloadIdentities: () => ({
    data: { issuers: [], admissions: mocks.admissions },
    isPending: mocks.isPending,
    isError: mocks.isError,
    refetch: vi.fn(),
  }),
  invalidateAllWorkloadIdentities: vi.fn(),
}));
vi.mock("@gram/client/react-query/withdrawWorkloadSubject.js", () => ({
  useWithdrawWorkloadSubjectMutation: () => ({
    mutate: mocks.withdraw,
    isPending: mocks.isWithdrawing,
  }),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: React.ReactNode }) => children,
}));
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

const agent = { id: "agent_1", name: "Release Bot" } as ManagedAgent;

function admission(over: Partial<WorkloadAdmission> = {}): WorkloadAdmission {
  return {
    id: "adm_1",
    agentId: "agent_1",
    agentName: "Release Bot",
    issuer: "https://issuer.example",
    issuerName: "Claude Tag",
    workloadIssuerId: "iss_1",
    matchKind: "wildcard",
    wildcardActive: true,
    name: "",
    subject: "spiffe://example/org/org-123/agent/*",
    tags: [],
    organizationId: "org_1",
    projectId: "",
    createdAt: new Date(),
    updatedAt: new Date(),
    ...over,
  } as WorkloadAdmission;
}

function renderPanel() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <AgentFederatedAccess agent={agent} />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  mocks.admissions = [];
  mocks.isPending = false;
  mocks.isError = false;
  mocks.isWithdrawing = false;
  mocks.withdraw.mockReset();
});

it("lists only the platforms admitted as this agent", () => {
  mocks.admissions = [
    admission(),
    admission({ id: "adm_2", agentId: "agent_other", issuerName: "Other" }),
  ];
  renderPanel();
  expect(screen.getByText("Claude Tag")).toBeTruthy();
  expect(screen.queryByText("Other")).toBeNull();
});

it("says so when no platform signs in as the agent", () => {
  renderPanel();
  expect(screen.getByText(/No platform signs in as this agent/)).toBeTruthy();
});

it("withdraws the admission once the removal is confirmed", () => {
  mocks.admissions = [admission()];
  renderPanel();
  fireEvent.click(
    screen.getByRole("button", { name: "Remove Claude Tag access" }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Remove access" }));
  expect(mocks.withdraw).toHaveBeenCalledWith({ request: { id: "adm_1" } });
});

it("does not withdraw anything until the dialog is confirmed", () => {
  mocks.admissions = [admission()];
  renderPanel();
  fireEvent.click(
    screen.getByRole("button", { name: "Remove Claude Tag access" }),
  );
  expect(mocks.withdraw).not.toHaveBeenCalled();
});

it("flags a wildcard rule the platform has not turned on", () => {
  mocks.admissions = [admission({ wildcardActive: false })];
  const { container } = renderPanel();
  expect(container.querySelector(".bg-warning")).toBeTruthy();
});
