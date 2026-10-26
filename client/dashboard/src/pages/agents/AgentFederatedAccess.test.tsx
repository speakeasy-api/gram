import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { WorkloadIssuer } from "@gram/client/models/components/workloadissuer.js";
import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";
import { unrecognized } from "@gram/client/types/unrecognized.js";
import { afterEach, expect, it, vi } from "vitest";
import { AgentFederatedAccess } from "./AgentFederatedAccess";

const mocks = vi.hoisted(() => ({
  admissions: [] as WorkloadAdmission[],
  issuers: [] as WorkloadIssuer[],
  isPending: false,
  isError: false,
  withdraw: vi.fn(),
  isWithdrawing: false,
  refetch: vi.fn(),
}));

vi.mock("@gram/client/react-query/workloadIdentities.js", () => ({
  useWorkloadIdentities: () => ({
    data: { issuers: mocks.issuers, admissions: mocks.admissions },
    isPending: mocks.isPending,
    isError: mocks.isError,
    refetch: mocks.refetch,
  }),
  invalidateAllWorkloadIdentities: vi.fn(),
}));
vi.mock("@gram/client/react-query/withdrawWorkloadSubject.js", () => ({
  useWithdrawWorkloadSubjectMutation: () => ({
    mutate: mocks.withdraw,
    isPending: mocks.isWithdrawing,
  }),
}));
vi.mock("@gram/client/react-query/updateWorkloadIssuer.js", () => ({
  useUpdateWorkloadIssuerMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
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
  const tree = () => (
    <QueryClientProvider client={client}>
      <AgentFederatedAccess agent={agent} />
    </QueryClientProvider>
  );
  const view = render(tree());
  // The hooks are mocked, so a flag flipped mid-test only reaches the panel on
  // a render it did not ask for itself. A fresh element each time, because
  // React bails out of re-rendering the one it already holds.
  return { ...view, redraw: () => view.rerender(tree()) };
}

afterEach(() => {
  cleanup();
  mocks.admissions = [];
  mocks.issuers = [];
  mocks.isPending = false;
  mocks.isError = false;
  mocks.isWithdrawing = false;
  mocks.withdraw.mockReset();
  mocks.refetch.mockReset();
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

it("waits rather than claiming nothing signs in while the read is in flight", () => {
  mocks.isPending = true;
  const { container } = renderPanel();
  expect(container.querySelector("[aria-busy=true]")).toBeTruthy();
  expect(screen.queryByText(/No platform signs in as this agent/)).toBeNull();
});

it("says a failed read failed, and retries it", () => {
  mocks.isError = true;
  renderPanel();
  expect(screen.getByText("This panel could not be loaded.")).toBeTruthy();
  expect(screen.queryByText(/No platform signs in as this agent/)).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Try again" }));
  expect(mocks.refetch).toHaveBeenCalledTimes(1);
});

it("names the removal in flight rather than offering it again", () => {
  mocks.admissions = [admission()];
  const { redraw } = renderPanel();
  fireEvent.click(
    screen.getByRole("button", { name: "Remove Claude Tag access" }),
  );
  mocks.isWithdrawing = true;
  redraw();

  const confirm = screen.getByRole("button", { name: "Removing…" });
  expect((confirm as HTMLButtonElement).disabled).toBe(true);
  expect(screen.queryByRole("button", { name: "Remove access" })).toBeNull();
});

it("flags a wildcard rule the platform has not turned on", () => {
  mocks.admissions = [admission({ wildcardActive: false })];
  const { container } = renderPanel();
  expect(container.querySelector(".bg-warning")).toBeTruthy();
});

it.each([
  ["exact", "One job"],
  ["wildcard", "Any job in the connected organization"],
  [unrecognized("future_match"), "Unknown job coverage"],
] as const)(
  "describes %s admission coverage without guessing",
  (matchKind, label) => {
    mocks.admissions = [admission({ matchKind })];
    const { container } = renderPanel();
    expect(screen.getByText(label)).toBeTruthy();
    expect(container.querySelector(".bg-warning")).toBeNull();
    if (matchKind !== "exact" && matchKind !== "wildcard") {
      expect(screen.queryByText("One job")).toBeNull();
      expect(
        screen.queryByText("Any job in the connected organization"),
      ).toBeNull();
      expect(screen.queryByText("future_match")).toBeNull();
    }
  },
);

it("renders one policy control per issuer while preserving every subject row", () => {
  const issuer: WorkloadIssuer = {
    id: "iss_1",
    name: "Example cluster",
    issuer: "https://issuer.example",
    jwksUri: "https://issuer.example/jwks",
    description: "",
    tags: [],
    allowAssertionReuse: false,
    allowWildcardAdmission: false,
    organizationId: "org_example",
    projectId: "",
    createdAt: new Date(),
    updatedAt: new Date(),
  };
  mocks.issuers = [
    issuer,
    { ...issuer, id: "iss_2", name: "Other cluster" },
    { ...issuer, id: "iss_unused", name: "Unused cluster" },
  ];
  mocks.admissions = [
    admission({ id: "adm_1", matchKind: "exact", subject: "workload-one" }),
    admission({ id: "adm_2", matchKind: "exact", subject: "workload-two" }),
    admission({
      id: "adm_3",
      workloadIssuerId: "iss_2",
      issuerName: "Other cluster",
    }),
  ];
  renderPanel();
  expect(
    screen.getAllByRole("button", {
      name: "Edit Example cluster issuer policy",
    }),
  ).toHaveLength(1);
  expect(
    screen.getAllByRole("button", { name: "Edit Other cluster issuer policy" }),
  ).toHaveLength(1);
  expect(
    screen.queryByRole("button", { name: "Edit Unused cluster issuer policy" }),
  ).toBeNull();
  expect(screen.getAllByText("Assertion reuse off (jti/uti)")).toHaveLength(2);
  expect(
    screen.getAllByRole("button", { name: /Remove .* access/ }),
  ).toHaveLength(3);
});
