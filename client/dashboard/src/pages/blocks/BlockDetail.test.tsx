import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  useGetBlock: vi.fn(),
  useSubmitMutation: vi.fn(),
  mutateAsync: vi.fn(),
  refetch: vi.fn(),
}));

// Only useParams is exercised by BlockPage; keep the rest of react-router real.
vi.mock("react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-router")>()),
  useParams: () => ({ id: "block-123" }),
}));

vi.mock("@gram/client/react-query/riskGetBlock.js", () => ({
  useRiskGetBlock: mocks.useGetBlock,
}));
vi.mock("@gram/client/react-query/riskSubmitBlockFeedback.js", () => ({
  useRiskSubmitBlockFeedbackMutation: mocks.useSubmitMutation,
}));

// A signed-in session so BlockPage renders the body rather than redirecting.
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ session: { id: "sess-1" } }),
}));

vi.mock("@/lib/utils", () => ({ buildLoginRedirectURL: () => "/login" }));

vi.mock("@/components/gram-logo", () => ({ GramLogo: () => null }));

vi.mock("@/components/ui/type", () => ({
  Type: ({ children }: { children: ReactNode }) => <span>{children}</span>,
}));

vi.mock("lucide-react", () => ({
  ThumbsUp: () => null,
  ThumbsDown: () => null,
  LoaderCircle: () => null,
  Shield: () => null,
}));

// Bundled icon imports can't resolve in the test env; stub the few
// primitives the page uses so Button renders as a plain <button>.
vi.mock("@/components/ui/button", () => {
  const Button = ({
    children,
    onClick,
    disabled,
  }: {
    children: ReactNode;
    onClick?: () => void;
    disabled?: boolean;
    variant?: string;
  }) => (
    <button onClick={onClick} disabled={disabled}>
      {children}
    </button>
  );
  Button.LeftIcon = ({ children }: { children: ReactNode }) => <>{children}</>;
  Button.Text = ({ children }: { children: ReactNode }) => <>{children}</>;
  return { Button };
});
vi.mock("@/components/ui/stack", () => ({
  Stack: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

import { BlockPage } from "./BlockDetail";

const sampleBlock = {
  id: "block-123",
  projectId: "proj-1",
  reason: `Speakeasy blocked this tool call: matched policy "Block Secrets" (Attempted to read .env secrets)`,
  policyName: "Block Secrets",
  toolName: "Bash",
  createdAt: "2026-06-24T21:00:00Z",
  feedback: undefined as string | undefined,
};

function mockLoadedBlock(block: Record<string, unknown>) {
  mocks.useGetBlock.mockReturnValue({
    data: block,
    isLoading: false,
    error: null,
    refetch: mocks.refetch,
  });
  mocks.useSubmitMutation.mockReturnValue({
    mutateAsync: mocks.mutateAsync,
    isPending: false,
  });
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("BlockPage", () => {
  it("shows the policy/tool header and the server-provided reason verbatim", () => {
    mockLoadedBlock(sampleBlock);

    render(<BlockPage />);

    expect(screen.getByText(/Blocked by policy/)).toBeTruthy();
    expect(screen.getByText(/tool Bash/)).toBeTruthy();
    // The reason box renders block.reason exactly as the backend stored it —
    // no client-side parsing of the message wording.
    expect(screen.getByText(sampleBlock.reason)).toBeTruthy();
  });

  it("submits 'up' feedback and refetches when Helpful is clicked", async () => {
    mockLoadedBlock(sampleBlock);
    mocks.mutateAsync.mockResolvedValue({});

    render(<BlockPage />);

    fireEvent.click(screen.getByRole("button", { name: "Helpful" }));

    await waitFor(() => {
      expect(mocks.mutateAsync).toHaveBeenCalledWith({
        request: {
          submitRiskBlockFeedbackRequestBody: {
            id: "block-123",
            sentiment: "up",
          },
        },
      });
    });
    await waitFor(() => expect(mocks.refetch).toHaveBeenCalledTimes(1));
  });

  it("confirms recorded feedback once a vote is present", () => {
    mockLoadedBlock({ ...sampleBlock, feedback: "down" });

    render(<BlockPage />);

    expect(screen.getByText("Thanks for the feedback.")).toBeTruthy();
  });

  it("shows an access/removed message when the block fails to load", () => {
    mocks.useGetBlock.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error("forbidden"),
      refetch: mocks.refetch,
    });
    mocks.useSubmitMutation.mockReturnValue({
      mutateAsync: mocks.mutateAsync,
      isPending: false,
    });

    render(<BlockPage />);

    expect(screen.getByText(/couldn't load this block/)).toBeTruthy();
  });
});
