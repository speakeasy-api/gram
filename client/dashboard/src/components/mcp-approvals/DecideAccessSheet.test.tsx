import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DecideAccessSheet } from "./DecideAccessSheet";

const mocks = vi.hoisted(() => ({
  createMutateAsync: vi.fn(),
  promoteMutateAsync: vi.fn(),
  decideMutateAsync: vi.fn(),
  invalidateInventory: vi.fn(),
  invalidateInventoryServer: vi.fn(),
  invalidateApprovalList: vi.fn(),
  invalidateApprovalGet: vi.fn(),
  toastError: vi.fn(),
  toastSuccess: vi.fn(),
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-id", slug: "test-project" }),
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@gram/client/react-query/createMcpApprovalRequest.js", () => ({
  useCreateMcpApprovalRequestMutation: () => ({
    isPending: false,
    mutateAsync: mocks.createMutateAsync,
  }),
}));

vi.mock("@gram/client/react-query/promoteMcpApprovalRequest.js", () => ({
  usePromoteMcpApprovalRequestMutation: () => ({
    isPending: false,
    mutateAsync: mocks.promoteMutateAsync,
  }),
}));

vi.mock("@gram/client/react-query/recordMcpApprovalDecision.js", () => ({
  useRecordMcpApprovalDecisionMutation: () => ({
    isPending: false,
    mutateAsync: mocks.decideMutateAsync,
  }),
}));

vi.mock("@gram/client/react-query/shadowMCPInventory.js", () => ({
  invalidateAllShadowMCPInventory: mocks.invalidateInventory,
}));

vi.mock("@gram/client/react-query/shadowMCPInventoryServer.js", () => ({
  invalidateAllShadowMCPInventoryServer: mocks.invalidateInventoryServer,
}));

vi.mock("@gram/client/react-query/listMcpApprovalRequests.js", () => ({
  invalidateAllListMcpApprovalRequests: mocks.invalidateApprovalList,
}));

vi.mock("@gram/client/react-query/getMcpApprovalRequest.js", () => ({
  invalidateGetMcpApprovalRequest: mocks.invalidateApprovalGet,
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({}),
}));

vi.mock("sonner", () => ({
  toast: {
    error: mocks.toastError,
    success: mocks.toastSuccess,
  },
}));

const target = {
  canonicalServerUrl: "https://mcp.example.com/mcp",
  displayName: "Example MCP",
};

function renderSheet(
  overrides: Partial<Parameters<typeof DecideAccessSheet>[0]> = {},
) {
  return render(
    <DecideAccessSheet
      target={target}
      open
      onOpenChange={vi.fn<(open: boolean) => void>()}
      disposition="block_all"
      members={[]}
      roles={[]}
      {...overrides}
    />,
  );
}

describe("DecideAccessSheet", () => {
  beforeEach(() => {
    mocks.createMutateAsync.mockResolvedValue({ id: "created-request-id" });
    mocks.promoteMutateAsync.mockResolvedValue({ id: "promoted-request-id" });
    mocks.decideMutateAsync.mockResolvedValue({});
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("records an approval on the existing request without opening a new one", async () => {
    renderSheet({
      target: { ...target, approvalRequestId: "existing-request-id" },
    });

    fireEvent.click(screen.getByRole("button", { name: "Approve Server" }));

    await waitFor(() => {
      expect(mocks.decideMutateAsync).toHaveBeenCalledTimes(1);
    });
    expect(mocks.createMutateAsync).not.toHaveBeenCalled();
    expect(mocks.decideMutateAsync).toHaveBeenCalledWith({
      request: {
        gramProject: "test-project",
        recordDecisionRequestBody: {
          id: "existing-request-id",
          decision: "approved",
          rationale: "Approved for use in this project.",
          grantedPrincipalUrns: undefined,
        },
      },
    });
  });

  it("opens a request first when the server has none, then decides on it", async () => {
    renderSheet();

    fireEvent.click(screen.getByRole("button", { name: "Approve Server" }));

    await waitFor(() => {
      expect(mocks.decideMutateAsync).toHaveBeenCalledTimes(1);
    });
    expect(mocks.createMutateAsync).toHaveBeenCalledWith({
      request: {
        gramProject: "test-project",
        createRequestRequestBody: {
          targetKind: "server_url",
          target: "https://mcp.example.com/mcp",
          note: "Approved for use in this project.",
        },
      },
    });
    expect(mocks.decideMutateAsync).toHaveBeenCalledWith(
      expect.objectContaining({
        request: expect.objectContaining({
          recordDecisionRequestBody: expect.objectContaining({
            id: "created-request-id",
          }),
        }),
      }),
    );
  });

  it("promotes a pending legacy request into the review before deciding", async () => {
    renderSheet({
      target: { ...target, pendingBypassRequestId: "legacy-bypass-id" },
    });

    fireEvent.click(screen.getByRole("button", { name: "Approve Server" }));

    await waitFor(() => {
      expect(mocks.decideMutateAsync).toHaveBeenCalledTimes(1);
    });
    expect(mocks.createMutateAsync).not.toHaveBeenCalled();
    expect(mocks.promoteMutateAsync).toHaveBeenCalledWith({
      request: {
        gramProject: "test-project",
        promoteRequestBody: {
          riskPolicyBypassRequestId: "legacy-bypass-id",
        },
      },
    });
    expect(mocks.decideMutateAsync).toHaveBeenCalledWith(
      expect.objectContaining({
        request: expect.objectContaining({
          recordDecisionRequestBody: expect.objectContaining({
            id: "promoted-request-id",
          }),
        }),
      }),
    );
  });

  it("denies with the deny prefill and closes the sheet", async () => {
    const onOpenChange = vi.fn<(open: boolean) => void>();
    renderSheet({
      target: { ...target, approvalRequestId: "existing-request-id" },
      onOpenChange,
    });

    fireEvent.click(screen.getByRole("radio", { name: /Deny/ }));
    fireEvent.click(screen.getByRole("button", { name: "Deny Server" }));

    await waitFor(() => {
      expect(mocks.decideMutateAsync).toHaveBeenCalledTimes(1);
    });
    expect(mocks.decideMutateAsync).toHaveBeenCalledWith(
      expect.objectContaining({
        request: expect.objectContaining({
          recordDecisionRequestBody: expect.objectContaining({
            decision: "denied",
            rationale: "Denied for use in this project.",
          }),
        }),
      }),
    );
    await waitFor(() => {
      expect(onOpenChange).toHaveBeenCalledWith(false);
    });
    expect(mocks.invalidateInventory).toHaveBeenCalled();
    expect(mocks.invalidateInventoryServer).toHaveBeenCalled();
  });

  it("hides the audience picker under an allow-by-default policy", () => {
    renderSheet({ disposition: "allow_all" });

    expect(screen.queryAllByText("Who the approval covers")).toHaveLength(0);
    expect(
      screen.getAllByText("Unblock the server for everyone in the project.")
        .length,
    ).toBeGreaterThan(0);
  });

  it("shows the audience picker when a block-by-default policy exists", () => {
    renderSheet({ disposition: "block_all" });

    expect(
      screen.getAllByText("Who the approval covers").length,
    ).toBeGreaterThan(0);
  });

  it("keeps the sheet open and reports an error when the decision fails", async () => {
    mocks.decideMutateAsync.mockRejectedValue(new Error("boom"));
    const onOpenChange = vi.fn<(open: boolean) => void>();
    renderSheet({
      target: { ...target, approvalRequestId: "existing-request-id" },
      onOpenChange,
    });

    fireEvent.click(screen.getByRole("button", { name: "Approve Server" }));

    await waitFor(() => {
      expect(mocks.toastError).toHaveBeenCalled();
    });
    expect(onOpenChange).not.toHaveBeenCalledWith(false);
    expect(mocks.invalidateInventory).not.toHaveBeenCalled();
  });
});
