import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  useGetBlock: vi.fn(),
  useSubmitMutation: vi.fn(),
  mutateAsync: vi.fn(),
  refetch: vi.fn(),
  switchScopes: vi.fn(),
  toastError: vi.fn(),
  session: {
    session: "sess-1",
    activeOrganizationId: "org-1",
    organizations: [
      {
        id: "org-1",
        slug: "organization-one",
        projects: [{ id: "proj-1", slug: "project-one" }],
      },
      {
        id: "org-2",
        slug: "organization-two",
        projects: [{ id: "proj-2", slug: "project-two" }],
      },
    ],
  },
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
  useSession: () => mocks.session,
}));

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({ auth: { switchScopes: mocks.switchScopes } }),
}));

vi.mock("@/routes", () => ({
  useRoutes: ({
    orgSlug,
    projectSlug,
  }: {
    orgSlug?: string;
    projectSlug?: string;
  }) => ({
    riskEvents: {
      href: () => `/${orgSlug}/projects/${projectSlug}/risk-events`,
    },
  }),
}));

vi.mock("sonner", () => ({
  toast: { error: mocks.toastError },
}));

vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  buildLoginRedirectURL: () => "/login",
}));

vi.mock("@/components/gram-logo", () => ({ GramLogo: () => null }));

vi.mock("@/components/ui/Text", () => ({
  Text: ({ children }: { children: ReactNode }) => <span>{children}</span>,
}));

vi.mock("lucide-react", () => ({
  ThumbsUp: () => null,
  ThumbsDown: () => null,
}));

// Icon pulls lucide dynamicIconImports, which cannot resolve in the test env;
// stub the few primitives the page uses so Button renders as a plain <button>.
vi.mock("@/components/ui/Button", () => {
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

vi.mock("@/components/ui/Icon", () => ({
  Icon: () => null,
}));

vi.mock("@/components/ui/Stack", () => ({
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

function renderPage() {
  return render(
    <MemoryRouter>
      <BlockPage />
    </MemoryRouter>,
  );
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("BlockPage", () => {
  it("shows the policy/tool header and the server-provided reason verbatim", () => {
    mockLoadedBlock(sampleBlock);

    renderPage();

    expect(screen.getByText(/Blocked by policy/)).toBeTruthy();
    expect(screen.getByText(/tool Bash/)).toBeTruthy();
    // The reason box renders block.reason exactly as the backend stored it —
    // no client-side parsing of the message wording.
    expect(screen.getByText(sampleBlock.reason)).toBeTruthy();
  });

  it("falls back to spend-rule framing when the block has no policy name", () => {
    // Spend-gate blocks carry no risk policy, so policyName is empty; the
    // headline must not render `Blocked by policy ""`.
    mockLoadedBlock({
      ...sampleBlock,
      policyName: "",
      reason: `Speakeasy blocked this tool call: spend rule "Intern hard limit" — budget resets Aug 31, 2026 00:00 UTC`,
    });

    renderPage();

    expect(screen.getByText(/Blocked by a Speakeasy spend rule/)).toBeTruthy();
    expect(screen.queryByText(/Blocked by policy/)).toBeNull();
  });

  it("submits 'up' feedback and refetches when Helpful is clicked", async () => {
    mockLoadedBlock(sampleBlock);
    mocks.mutateAsync.mockResolvedValue({});

    renderPage();

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

    renderPage();

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

    renderPage();

    expect(screen.getByText(/couldn't load this block/)).toBeTruthy();
  });

  it("links to the risk event log for the block's project", () => {
    mockLoadedBlock(sampleBlock);

    renderPage();

    expect(
      screen
        .getByRole("link", { name: "View risk event log" })
        .getAttribute("href"),
    ).toBe("/organization-one/projects/project-one/risk-events");
  });

  it("switches organization before opening another organization's risk events", async () => {
    mockLoadedBlock({ ...sampleBlock, projectId: "proj-2" });
    mocks.switchScopes.mockResolvedValue({});
    const assign = vi
      .spyOn(window.location, "assign")
      .mockImplementation(() => {});

    renderPage();
    fireEvent.click(screen.getByRole("link", { name: "View risk event log" }));

    await waitFor(() => {
      expect(mocks.switchScopes).toHaveBeenCalledWith({
        organizationId: "org-2",
      });
      expect(assign).toHaveBeenCalledWith(
        "/organization-two/projects/project-two/risk-events",
      );
    });
    assign.mockRestore();
  });

  it("reports a failed organization switch", async () => {
    mockLoadedBlock({ ...sampleBlock, projectId: "proj-2" });
    mocks.switchScopes.mockRejectedValue(new Error("unavailable"));

    renderPage();
    fireEvent.click(screen.getByRole("link", { name: "View risk event log" }));

    await waitFor(() => {
      expect(mocks.toastError).toHaveBeenCalledWith(
        "Unable to switch organizations. Please try again.",
      );
    });
  });
});
