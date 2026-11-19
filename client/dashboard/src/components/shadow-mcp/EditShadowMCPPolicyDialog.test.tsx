import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { ServiceError } from "@gram/client/models/errors/serviceerror.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { EditShadowMCPPolicyDialog } from "./EditShadowMCPPolicyDialog";
import { testAccessSummary } from "./shadowMCPInventoryTestFixtures";
import type { ShadowMCPModeState } from "./shadowMCPMode";

const mocks = vi.hoisted(() => ({
  servers: [] as unknown[],
  createPolicy: vi.fn(),
  updatePolicy: vi.fn(),
  removePolicy: vi.fn(),
  createRequest: vi.fn(),
  promoteRequest: vi.fn(),
  decide: vi.fn(),
  calls: [] as string[],
  toastSuccess: vi.fn(),
}));

const { mutation } = vi.hoisted(() => ({
  mutation: (name: string, fn: () => (args: unknown) => unknown) => () => ({
    isPending: false,
    mutateAsync: async (args: unknown) => {
      mocks.calls.push(name);
      return fn()(args);
    },
  }),
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-id", slug: "test-project" }),
}));

vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));

vi.mock("@gram/client/react-query/shadowMCPInventory.js", () => ({
  buildShadowMCPInventoryQuery: (_client: unknown, request: object) => ({
    queryKey: ["inventory", request],
    queryFn: async () => ({ servers: mocks.servers }),
  }),
  invalidateAllShadowMCPInventory: vi.fn(),
}));

vi.mock("@gram/client/react-query/shadowMCPInventoryServer.js", () => ({
  invalidateAllShadowMCPInventoryServer: vi.fn(),
}));
vi.mock("@gram/client/react-query/listMcpApprovalRequests.js", () => ({
  invalidateAllListMcpApprovalRequests: vi.fn(),
}));
vi.mock("@gram/client/react-query/riskListPolicies.js", () => ({
  invalidateAllRiskListPolicies: vi.fn(),
}));
vi.mock("@/components/shadow-mcp/useShadowMCPPolicyInventory", () => ({
  invalidateShadowMCPPolicyInventory: vi.fn(),
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({ data: { members: [] } }),
}));
vi.mock("@gram/client/react-query/roles.js", () => ({
  useRoles: () => ({ data: { roles: [] } }),
}));

vi.mock("@gram/client/react-query/riskCreatePolicy.js", () => ({
  useRiskCreatePolicyMutation: mutation(
    "createPolicy",
    () => mocks.createPolicy,
  ),
}));
vi.mock("@gram/client/react-query/riskPoliciesUpdate.js", () => ({
  useRiskPoliciesUpdateMutation: mutation(
    "updatePolicy",
    () => mocks.updatePolicy,
  ),
}));
vi.mock("@gram/client/react-query/riskPoliciesDelete.js", () => ({
  useRiskPoliciesDeleteMutation: mutation(
    "removePolicy",
    () => mocks.removePolicy,
  ),
}));
vi.mock("@gram/client/react-query/createMcpApprovalRequest.js", () => ({
  useCreateMcpApprovalRequestMutation: mutation(
    "createRequest",
    () => mocks.createRequest,
  ),
}));
vi.mock("@gram/client/react-query/promoteMcpApprovalRequest.js", () => ({
  usePromoteMcpApprovalRequestMutation: mutation(
    "promoteRequest",
    () => mocks.promoteRequest,
  ),
}));
vi.mock("@gram/client/react-query/recordMcpApprovalDecision.js", () => ({
  useRecordMcpApprovalDecisionMutation: mutation("decide", () => mocks.decide),
}));

vi.mock("sonner", () => ({
  toast: { success: mocks.toastSuccess, error: vi.fn() },
}));

vi.mock("@/components/ui/MultiSelect", () => ({
  MultiSelect: () => <div data-testid="audience-select" />,
}));

function server(
  name: string,
  overrides: Partial<ShadowMCPInventoryServer> = {},
): ShadowMCPInventoryServer {
  return {
    canonicalServerUrl: `https://${name}.example.com/mcp`,
    serverSlug: name,
    urlHost: `${name}.example.com`,
    serverName: name,
    firstSeen: new Date("2026-11-01T00:00:00Z"),
    lastSeen: new Date("2026-11-18T00:00:00Z"),
    observedUseCount: 0,
    blockedCallCount: 0,
    userCount: 0,
    topUsers: [],
    access: "allowed",
    accessSummary: testAccessSummary("allowed"),
    requestCount: 0,
    allowedPolicyIds: [],
    blockedPolicyIds: [],
    ...overrides,
  };
}

const allowed: ShadowMCPModeState = {
  mode: "allowed",
  policy: undefined,
  heldBy: null,
};

function renderDialog(
  state: ShadowMCPModeState = allowed,
  onOpenChange = vi.fn<(open: boolean) => void>(),
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <EditShadowMCPPolicyDialog
        open
        onOpenChange={onOpenChange}
        state={state}
      />
    </QueryClientProvider>,
  );
  return onOpenChange;
}

function chooseBlockAndReview() {
  fireEvent.click(screen.getByRole("radio", { name: /Block by default/ }));
  fireEvent.click(screen.getByRole("button", { name: "Review changes" }));
}

describe("EditShadowMCPPolicyDialog", () => {
  beforeEach(() => {
    mocks.calls = [];
    mocks.servers = [
      server("quiet"),
      server("busy", { observedUseCount: 40 }),
      server("blocked-a-lot", { observedUseCount: 3, blockedCallCount: 12 }),
    ];
    mocks.createPolicy.mockResolvedValue({});
    mocks.removePolicy.mockResolvedValue({});
    mocks.createRequest.mockResolvedValue({ id: "new-request" });
    mocks.decide.mockResolvedValue({});
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("lists the servers that change, most blocked and most used first", async () => {
    renderDialog();
    chooseBlockAndReview();

    await screen.findByText("blocked-a-lot");
    const rows = screen.getAllByRole("row").slice(1);
    expect(
      rows.map((row) => within(row).getAllByRole("cell")[0]?.textContent),
    ).toEqual([
      "blocked-a-lotblocked-a-lot.example.com",
      "busybusy.example.com",
      "quietquiet.example.com",
    ]);
    expect(
      screen.getByText(/3 servers have no decision yet and will be blocked/),
    ).toBeTruthy();
  });

  it("blocks by default without recording decisions when nothing is kept", async () => {
    const onOpenChange = renderDialog();
    chooseBlockAndReview();
    await screen.findByText("busy");

    fireEvent.click(screen.getByRole("button", { name: "Block by default" }));

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
    expect(mocks.calls).toEqual(["createPolicy"]);
    expect(mocks.toastSuccess).toHaveBeenCalledWith(
      "MCP servers are now blocked by default.",
    );
  });

  it("approves the kept servers before it blocks by default", async () => {
    const onOpenChange = renderDialog();
    chooseBlockAndReview();
    await screen.findByText("busy");

    fireEvent.click(screen.getByRole("button", { name: "Advanced" }));
    fireEvent.click(
      screen.getByRole("checkbox", { name: "Keep busy as it is today" }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Block by default and approve 1" }),
    );

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
    expect(mocks.calls).toEqual(["createRequest", "decide", "createPolicy"]);
    expect(mocks.decide).toHaveBeenCalledWith({
      request: {
        gramProject: "test-project",
        recordDecisionRequestBody: {
          id: "new-request",
          decision: "approved",
          rationale:
            "Approved so it keeps working when servers are blocked by default.",
          grantedPrincipalUrns: undefined,
        },
      },
    });
  });

  it("does not change the policy when a decision fails", async () => {
    mocks.decide.mockRejectedValueOnce(new Error("boom"));
    const onOpenChange = renderDialog();
    chooseBlockAndReview();
    await screen.findByText("busy");

    fireEvent.click(screen.getByRole("button", { name: "Advanced" }));
    fireEvent.click(
      screen.getByRole("checkbox", { name: "Keep busy as it is today" }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Block by default and approve 1" }),
    );

    await screen.findByText(/Recording a decision on busy failed/);
    expect(mocks.calls).not.toContain("createPolicy");
    expect(onOpenChange).not.toHaveBeenCalledWith(false);
  });

  it("shows the server's refusal and stays open", async () => {
    mocks.createPolicy.mockRejectedValueOnce(
      new ServiceError(
        {
          name: "bad_request",
          id: "err-1",
          message:
            "This project has standing denials scoped to specific people (https://busy.example.com/mcp), and a block-by-default policy for everyone blocks those servers for everyone.",
          temporary: false,
          timeout: false,
          fault: false,
        },
        {
          response: new Response(null, { status: 400 }),
          request: new Request("https://example.test"),
          body: "",
        },
      ),
    );
    const onOpenChange = renderDialog();
    chooseBlockAndReview();
    await screen.findByText("busy");

    fireEvent.click(screen.getByRole("button", { name: "Block by default" }));

    await screen.findByText(/standing denials scoped to specific people/);
    expect(onOpenChange).not.toHaveBeenCalledWith(false);
  });

  it("keeps servers blocked for the same people a narrowed policy blocks", async () => {
    const onOpenChange = renderDialog({
      mode: "blocked",
      policy: {
        id: "policy-1",
        name: "Shadow MCP",
        sources: ["shadow_mcp"],
        audienceType: "targeted",
        audiencePrincipalUrns: ["role:organization:contractors"],
      } as unknown as ShadowMCPModeState["policy"],
      heldBy: null,
    });
    fireEvent.click(screen.getByRole("radio", { name: /Allow by default/ }));
    fireEvent.click(screen.getByRole("button", { name: "Review changes" }));
    await screen.findByText("busy");

    fireEvent.click(screen.getByRole("button", { name: "Advanced" }));
    fireEvent.click(
      screen.getByRole("checkbox", { name: "Keep busy as it is today" }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Allow by default and block 1" }),
    );

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
    expect(mocks.decide).toHaveBeenCalledWith(
      expect.objectContaining({
        request: expect.objectContaining({
          recordDecisionRequestBody: expect.objectContaining({
            decision: "denied",
            grantedPrincipalUrns: ["role:organization:contractors"],
          }),
        }),
      }),
    );
    expect(mocks.calls).toEqual(["createRequest", "decide", "removePolicy"]);
  });

  it("says it is finding servers while the list loads", async () => {
    renderDialog();
    chooseBlockAndReview();
    expect(
      screen.getByText("Finding the servers this change affects…"),
    ).toBeTruthy();
    await screen.findByText("busy");
  });

  it("removes the policy to allow by default", async () => {
    const onOpenChange = renderDialog({
      mode: "blocked",
      policy: {
        id: "policy-1",
        name: "Shadow MCP",
        sources: ["shadow_mcp"],
        audienceType: "everyone",
        audiencePrincipalUrns: [],
      } as unknown as ShadowMCPModeState["policy"],
      heldBy: null,
    });
    fireEvent.click(screen.getByRole("radio", { name: /Allow by default/ }));
    fireEvent.click(screen.getByRole("button", { name: "Review changes" }));
    await screen.findByText("busy");
    expect(screen.getAllByText("Blocked → Allowed")).toHaveLength(3);

    fireEvent.click(screen.getByRole("button", { name: "Allow by default" }));

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
    expect(mocks.calls).toEqual(["removePolicy"]);
  });
});
