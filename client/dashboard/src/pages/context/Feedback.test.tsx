import { describe, test, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { FeedbackPanel } from "./FeedbackPanel";

function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
}

function Wrapper({
  children,
  queryClient,
}: {
  children: React.ReactNode;
  queryClient: QueryClient;
}) {
  return (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
}

const MOCK_FEEDBACK = {
  upvotes: 12,
  downvotes: 3,
  labels: ["helpful", "accurate"],
  userVote: null,
  comments: [],
};

const MOCK_COMMENTS = [
  {
    id: "c1",
    author: "alice",
    authorType: "human" as const,
    content: "Great documentation!",
    createdAt: "2026-04-01T10:00:00Z",
    upvotes: 5,
    downvotes: 0,
  },
  {
    id: "c2",
    author: "doc-bot",
    authorType: "agent" as const,
    content: "Added cross-references to related pages.",
    createdAt: "2026-04-02T14:30:00Z",
    upvotes: 2,
    downvotes: 1,
  },
];

describe("FeedbackPanel", () => {
  let fetchSpy: ReturnType<typeof vi.spyOn>;
  let queryClient: QueryClient;

  beforeEach(() => {
    queryClient = createQueryClient();
    fetchSpy = vi.spyOn(globalThis, "fetch");
  });

  afterEach(() => {
    fetchSpy.mockRestore();
  });

  test("renders vote counts from API", async () => {
    fetchSpy.mockImplementation(async (input) => {
      const url = typeof input === "string" ? input : (input as Request).url;
      if (url.includes("/rpc/corpus.getFeedback")) {
        return new Response(JSON.stringify(MOCK_FEEDBACK), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url.includes("/rpc/corpus.listComments")) {
        return new Response(JSON.stringify({ comments: MOCK_COMMENTS }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      return new Response("Not Found", { status: 404 });
    });

    render(
      <Wrapper queryClient={queryClient}>
        <FeedbackPanel filePath="docs/guide.md" />
      </Wrapper>,
    );

    await waitFor(() => {
      expect(screen.getByText("12")).toBeInTheDocument();
    });
    expect(screen.getByText("3")).toBeInTheDocument();
  });

  test("clicking upvote calls vote API", async () => {
    const user = userEvent.setup();

    fetchSpy.mockImplementation(async (input) => {
      const url = typeof input === "string" ? input : (input as Request).url;
      if (url.includes("/rpc/corpus.getFeedback")) {
        return new Response(JSON.stringify(MOCK_FEEDBACK), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url.includes("/rpc/corpus.listComments")) {
        return new Response(JSON.stringify({ comments: [] }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url.includes("/rpc/corpus.voteFeedback")) {
        return new Response(JSON.stringify({ ok: true }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      return new Response("Not Found", { status: 404 });
    });

    render(
      <Wrapper queryClient={queryClient}>
        <FeedbackPanel filePath="docs/guide.md" />
      </Wrapper>,
    );

    await waitFor(() => {
      expect(screen.getByText("12")).toBeInTheDocument();
    });

    const upvoteButton = screen.getByRole("button", { name: /upvote/i });
    await user.click(upvoteButton);

    await waitFor(() => {
      const calls = fetchSpy.mock.calls.filter((call) => {
        const url =
          typeof call[0] === "string" ? call[0] : (call[0] as Request).url;
        return url.includes("/rpc/corpus.voteFeedback");
      });
      expect(calls).toHaveLength(1);
    });
  });

  test("renders comment thread", async () => {
    fetchSpy.mockImplementation(async (input) => {
      const url = typeof input === "string" ? input : (input as Request).url;
      if (url.includes("/rpc/corpus.getFeedback")) {
        return new Response(JSON.stringify(MOCK_FEEDBACK), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url.includes("/rpc/corpus.listComments")) {
        return new Response(JSON.stringify({ comments: MOCK_COMMENTS }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      return new Response("Not Found", { status: 404 });
    });

    render(
      <Wrapper queryClient={queryClient}>
        <FeedbackPanel filePath="docs/guide.md" />
      </Wrapper>,
    );

    await waitFor(() => {
      expect(screen.getByText("Great documentation!")).toBeInTheDocument();
    });

    expect(
      screen.getByText("Added cross-references to related pages."),
    ).toBeInTheDocument();
    expect(screen.getByText("alice")).toBeInTheDocument();
    expect(screen.getByText("doc-bot")).toBeInTheDocument();

    // Agent badge should be present for doc-bot
    expect(screen.getByText("Agent")).toBeInTheDocument();
  });
});
