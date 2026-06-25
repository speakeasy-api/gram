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

// Only useParams is exercised by BlockDetailPage; keep the rest of react-router real.
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

vi.mock("@/contexts/Auth", () => ({ useSession: () => ({ session: null }) }));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@/components/page-layout", () => {
  const Page = ({ children }: { children: ReactNode }) => <div>{children}</div>;
  const Header = ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  );
  Header.Title = ({ children }: { children: ReactNode }) => <h1>{children}</h1>;
  Page.Header = Header;
  Page.Body = ({ children }: { children: ReactNode }) => <div>{children}</div>;
  return { Page };
});

vi.mock("@/components/ui/type", () => ({
  Type: ({ children }: { children: ReactNode }) => <span>{children}</span>,
}));

vi.mock("lucide-react", () => ({
  ThumbsUp: () => null,
  ThumbsDown: () => null,
}));

// moonshine pulls lucide dynamicIconImports which can't resolve in the test env;
// stub the few primitives the page uses so Button renders as a plain <button>.
vi.mock("@speakeasy-api/moonshine", () => {
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
  return {
    Button,
    Icon: () => null,
    Stack: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  };
});

import { BlockDetailPage } from "./BlockDetail";

const sampleBlock = {
  id: "block-123",
  projectId: "proj-1",
  reason: `Speakeasy blocked this tool call: matched policy "Block Secrets"`,
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

describe("BlockDetailPage", () => {
  it("renders the block reason, policy name, and tool", () => {
    mockLoadedBlock(sampleBlock);

    render(<BlockDetailPage />);

    expect(screen.getByText(/matched policy/)).toBeTruthy();
    expect(screen.getByText(/Blocked by policy/)).toBeTruthy();
    expect(screen.getByText(/tool Bash/)).toBeTruthy();
  });

  it("submits 'up' feedback and refetches when Helpful is clicked", async () => {
    mockLoadedBlock(sampleBlock);
    mocks.mutateAsync.mockResolvedValue({});

    render(<BlockDetailPage />);

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

    render(<BlockDetailPage />);

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

    render(<BlockDetailPage />);

    expect(screen.getByText(/couldn't load this block/)).toBeTruthy();
  });
});
