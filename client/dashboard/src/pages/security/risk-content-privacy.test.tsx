import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { FindingContext } from "./FindingContext";
import { FindingPayload } from "./FindingPayload";
import { TooltipProvider } from "@/components/ui/Tooltip";
import {
  act,
  cleanup,
  fireEvent,
  render,
  renderHook,
  screen,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  EventMatchDialog,
  MaskedMatch,
  RevealAllProvider,
  RevealAllToggle,
} from "./risk-ui";
import { useRiskContentRestricted } from "./use-risk-content-restricted";
import { useUnmaskedMatch } from "./unmask";
import { useExecutionPayload } from "./use-execution-payload";

const state = vi.hoisted(() => ({
  session: {
    organizationOverride: false,
    impersonatorEmail: "",
    activeOrganizationId: "org_fixture",
  },
  unmask: vi.fn(),
  payload: vi.fn<() => void>(),
  loadChat: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({ useSession: () => state.session }));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true }),
}));
vi.mock("@gram/client/react-query/riskUnmaskResult.js", () => ({
  useRiskUnmaskResultMutation: () => ({
    mutate: state.unmask,
    isPending: false,
    isError: false,
  }),
}));
vi.mock("@gram/client/react-query/riskRevealResultPayload.js", () => ({
  useRiskRevealResultPayloadMutation: () => ({
    mutate: state.payload,
    isPending: false,
    isError: false,
  }),
}));
vi.mock("@/components/code", () => ({ CodeBlock: () => null }));
vi.mock("@gram/client/react-query/loadChat.js", () => ({
  useLoadChat: (...args: unknown[]) => {
    state.loadChat(...args);
    return { data: undefined, isLoading: false, isError: false, error: null };
  },
}));

const finding: RiskResult = {
  id: "finding",
  policyId: "policy",
  policyVersion: 1,
  createdAt: new Date(0),
  source: "presidio",
  chatId: "chat",
  chatMessageId: "message",
  executionId: "execution",
  phase: "response",
};

afterEach(cleanup);
beforeEach(() => {
  state.session = {
    organizationOverride: false,
    impersonatorEmail: "",
    activeOrganizationId: "org_fixture",
  };
  state.unmask.mockReset();
  state.payload.mockReset();
  state.loadChat.mockReset();
});

describe("risk content privacy", () => {
  it.each(["ordinary", "support", "workos", "demo support", "demo workos"])(
    "matches the server policy for %s sessions",
    (session) => {
      state.session.organizationOverride = session.includes("support");
      state.session.impersonatorEmail = session.includes("workos")
        ? "operator@example.test"
        : "";
      if (session.startsWith("demo"))
        state.session.activeOrganizationId = "org_gram_demo_workspace";
      const { result } = renderHook(useRiskContentRestricted);
      expect(result.current).toBe(
        session === "support" || session === "workos",
      );
    },
  );

  it.each(["support", "workos"])(
    "replaces reveal controls with noninteractive notices for %s",
    (session) => {
      state.session.organizationOverride = session === "support";
      state.session.impersonatorEmail =
        session === "workos" ? "operator@example.test" : "";
      render(
        <TooltipProvider>
          <RevealAllProvider>
            <RevealAllToggle />
            <MaskedMatch resultId="finding" matchRedacted="<redacted len=12>" />
            <EventMatchDialog
              resultId="judge"
              matchRedacted="<redacted len=12>"
              rationale="Policy verdict"
            />
          </RevealAllProvider>
        </TooltipProvider>,
      );
      expect(screen.getAllByText("Hidden during impersonation")).toHaveLength(
        3,
      );
      expect(screen.queryByRole("button")).toBeNull();
      expect(screen.queryByText("Click to reveal")).toBeNull();
      for (const notice of screen.getAllByText("Hidden during impersonation"))
        fireEvent.click(notice);
      expect(state.unmask).not.toHaveBeenCalled();
      expect(screen.getByText("Policy verdict")).toBeTruthy();
    },
  );

  it("keeps drawer content locked and skips transcript loading", () => {
    state.session.organizationOverride = true;
    render(
      <>
        <FindingContext
          result={finding}
          kind="chat"
          chatResults={[]}
          chatResultsComplete
          revealed
          canReveal
          rating={null}
          onToggleReveal={vi.fn<() => void>()}
          onRequestReveal={vi.fn<() => void>()}
        />
        <FindingPayload
          result={finding}
          siblings={[]}
          siblingsComplete
          revealed
          canReveal
          payload={{
            data: undefined,
            isPending: false,
            isError: false,
            reveal: state.payload,
          }}
          onToggleReveal={vi.fn<() => void>()}
          onSelect={vi.fn<(id: string | null) => void>()}
        />
      </>,
    );
    expect(screen.getAllByText("Hidden during impersonation")).toHaveLength(2);
    expect(screen.queryByRole("button")).toBeNull();
    expect(state.loadChat).toHaveBeenCalledWith(
      expect.anything(),
      undefined,
      expect.objectContaining({ enabled: false }),
    );
    expect(state.unmask).not.toHaveBeenCalled();
    expect(state.payload).not.toHaveBeenCalled();
  });

  it("blocks reveal callbacks and hides a cached match when support access begins", () => {
    state.unmask.mockImplementation((_req, opts) =>
      opts.onSuccess({ match: "fixture secret" }),
    );
    const { result, rerender } = renderHook(() => ({
      match: useUnmaskedMatch("finding"),
      payload: useExecutionPayload(finding),
    }));
    act(() => result.current.match.reveal());
    expect(result.current.match.value).toBe("fixture secret");
    state.unmask.mockClear();
    state.session.organizationOverride = true;
    rerender();
    expect(result.current.match.value).toBeNull();
    act(() => {
      result.current.match.reveal();
      result.current.payload.reveal();
    });
    expect(state.unmask).not.toHaveBeenCalled();
    expect(state.payload).not.toHaveBeenCalled();
  });
});
