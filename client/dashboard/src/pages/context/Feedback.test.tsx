import { describe, test, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  createTestQueryClient,
  extractFetchUrl,
  TestQueryWrapper,
} from "@/test-utils";
import type { QueryClient } from "@tanstack/react-query";
import { getFeedbackFixture } from "@/hooks/feedback-fixtures";
import { FeedbackPanel } from "./FeedbackPanel";

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
    queryClient = createTestQueryClient();
    fetchSpy = vi.spyOn(globalThis, "fetch");
  });

  afterEach(() => {
    fetchSpy.mockRestore();
  });

  test("renders vote counts from API", async () => {
    const requestBodies = new Map<string, Record<string, unknown>>();

    fetchSpy.mockImplementation(async (input: RequestInfo | URL) => {
      const url = extractFetchUrl(input);
      if (input instanceof Request) {
        requestBodies.set(
          url,
          (await input.clone().json()) as Record<string, unknown>,
        );
      }
      if (url.includes("/rpc/corpus.getFeedback")) {
        return new Response(JSON.stringify(MOCK_FEEDBACK), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url.includes("/rpc/corpus.listComments")) {
        return new Response(
          JSON.stringify({
            comments: MOCK_COMMENTS.map((comment) => ({
              author: comment.author,
              author_type: comment.authorType,
              content: comment.content,
              created_at: comment.createdAt,
              downvotes: comment.downvotes,
              id: comment.id,
              upvotes: comment.upvotes,
            })),
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }
      return new Response("Not Found", { status: 404 });
    });

    render(
      <TestQueryWrapper queryClient={queryClient}>
        <FeedbackPanel filePath="docs/guide.md" />
      </TestQueryWrapper>,
    );

    await waitFor(() => {
      expect(screen.getByText("12")).toBeInTheDocument();
    });
    expect(screen.getByText("3")).toBeInTheDocument();

    const feedbackRequest = fetchSpy.mock.calls.find(
      (call: [RequestInfo | URL]) =>
        extractFetchUrl(call[0]).includes("/rpc/corpus.getFeedback"),
    );

    expect(feedbackRequest).toBeDefined();
    expect(requestBodies.get(extractFetchUrl(feedbackRequest?.[0]))).toEqual({
      file_path: "docs/guide.md",
    });
  });

  test("clicking upvote calls vote API", async () => {
    const user = userEvent.setup();
    const requestBodies = new Map<string, Record<string, unknown>>();

    fetchSpy.mockImplementation(async (input: RequestInfo | URL) => {
      const url = extractFetchUrl(input);
      if (input instanceof Request) {
        requestBodies.set(
          url,
          (await input.clone().json()) as Record<string, unknown>,
        );
      }
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
        return new Response(
          JSON.stringify({
            upvotes: 13,
            downvotes: 3,
            labels: ["helpful", "accurate"],
            user_vote: "up",
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }
      return new Response("Not Found", { status: 404 });
    });

    render(
      <TestQueryWrapper queryClient={queryClient}>
        <FeedbackPanel filePath="docs/guide.md" />
      </TestQueryWrapper>,
    );

    await waitFor(() => {
      expect(screen.getByText("12")).toBeInTheDocument();
    });

    const upvoteButton = screen.getByRole("button", { name: /upvote/i });
    await user.click(upvoteButton);

    await waitFor(() => {
      const calls = fetchSpy.mock.calls.filter((call: [RequestInfo | URL]) =>
        extractFetchUrl(call[0]).includes("/rpc/corpus.voteFeedback"),
      );
      expect(calls).toHaveLength(1);
    });

    const voteRequest = fetchSpy.mock.calls.find((call: [RequestInfo | URL]) =>
      extractFetchUrl(call[0]).includes("/rpc/corpus.voteFeedback"),
    );

    expect(voteRequest).toBeDefined();
    expect(requestBodies.get(extractFetchUrl(voteRequest?.[0]))).toEqual({
      direction: "up",
      file_path: "docs/guide.md",
    });
  });

  test("renders comment thread", async () => {
    const requestBodies = new Map<string, Record<string, unknown>>();

    fetchSpy.mockImplementation(async (input: RequestInfo | URL) => {
      const url = extractFetchUrl(input);
      if (input instanceof Request) {
        requestBodies.set(
          url,
          (await input.clone().json()) as Record<string, unknown>,
        );
      }
      if (url.includes("/rpc/corpus.getFeedback")) {
        return new Response(JSON.stringify(MOCK_FEEDBACK), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url.includes("/rpc/corpus.listComments")) {
        return new Response(
          JSON.stringify({
            comments: MOCK_COMMENTS.map((comment) => ({
              author: comment.author,
              author_type: comment.authorType,
              content: comment.content,
              created_at: comment.createdAt,
              downvotes: comment.downvotes,
              id: comment.id,
              upvotes: comment.upvotes,
            })),
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }
      return new Response("Not Found", { status: 404 });
    });

    render(
      <TestQueryWrapper queryClient={queryClient}>
        <FeedbackPanel filePath="docs/guide.md" />
      </TestQueryWrapper>,
    );

    await waitFor(() => {
      expect(screen.getByText("Great documentation!")).toBeInTheDocument();
    });

    expect(
      screen.getByText("Added cross-references to related pages."),
    ).toBeInTheDocument();
    expect(screen.getByText("alice")).toBeInTheDocument();
    expect(screen.getByText("doc-bot")).toBeInTheDocument();
    expect(screen.getByText("Agent")).toBeInTheDocument();

    const commentsRequest = fetchSpy.mock.calls.find(
      (call: [RequestInfo | URL]) =>
        extractFetchUrl(call[0]).includes("/rpc/corpus.listComments"),
    );

    expect(commentsRequest).toBeDefined();
    expect(requestBodies.get(extractFetchUrl(commentsRequest?.[0]))).toEqual({
      file_path: "docs/guide.md",
    });
  });

  test("falls back to local fixture data on permission denied", async () => {
    const user = userEvent.setup();
    const fixture = getFeedbackFixture("README.md");

    fetchSpy.mockImplementation(async (input: RequestInfo | URL) => {
      const url = extractFetchUrl(input);

      if (
        url.includes("/rpc/corpus.getFeedback") ||
        url.includes("/rpc/corpus.listComments")
      ) {
        return new Response(
          JSON.stringify({
            fault: false,
            id: "denied",
            message: "permission denied",
            name: "ServiceError",
            temporary: false,
            timeout: false,
          }),
          {
            status: 403,
            headers: { "Content-Type": "application/json" },
          },
        );
      }

      if (url.includes("/rpc/corpus.voteFeedback")) {
        throw new Error(
          "vote should not be called when fallback data is active",
        );
      }

      return new Response("Not Found", { status: 404 });
    });

    render(
      <TestQueryWrapper queryClient={queryClient}>
        <FeedbackPanel filePath="README.md" />
      </TestQueryWrapper>,
    );

    await waitFor(() => {
      expect(
        screen.getByText(String(fixture.feedback.upvotes)),
      ).toBeInTheDocument();
    });

    expect(
      screen.getByText(String(fixture.feedback.downvotes)),
    ).toBeInTheDocument();
    expect(screen.getByText(fixture.comments[0].content)).toBeInTheDocument();

    const upvoteButton = screen
      .getAllByRole("button", { name: /upvote/i })
      .find((button) => button.hasAttribute("disabled"));

    expect(upvoteButton).toBeDisabled();

    await user.click(upvoteButton!);

    const voteCalls = fetchSpy.mock.calls.filter((call: [RequestInfo | URL]) =>
      extractFetchUrl(call[0]).includes("/rpc/corpus.voteFeedback"),
    );

    expect(voteCalls).toHaveLength(0);
  });
});
