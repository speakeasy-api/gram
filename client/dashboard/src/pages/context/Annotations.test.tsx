import { describe, test, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  createTestQueryClient,
  extractFetchUrl,
  TestQueryWrapper,
} from "@/test-utils";
import type { QueryClient } from "@tanstack/react-query";
import { AnnotationsPanel } from "./AnnotationsPanel";

const MOCK_ANNOTATIONS = [
  {
    id: "a1",
    author: "jane",
    authorType: "human" as const,
    content: "This section needs a code example.",
    createdAt: "2026-04-05T09:00:00Z",
  },
  {
    id: "a2",
    author: "review-bot",
    authorType: "agent" as const,
    content: "Consider adding a warning about rate limits.",
    createdAt: "2026-04-06T11:15:00Z",
  },
];

describe("AnnotationsPanel", () => {
  let fetchSpy: ReturnType<typeof vi.spyOn>;
  let queryClient: QueryClient;

  beforeEach(() => {
    queryClient = createTestQueryClient();
    fetchSpy = vi.spyOn(globalThis, "fetch");
  });

  afterEach(() => {
    fetchSpy.mockRestore();
  });

  test("renders annotation list from API", async () => {
    fetchSpy.mockImplementation(async (input) => {
      const url = extractFetchUrl(input);
      if (url.includes("/rpc/corpus.listAnnotations")) {
        return new Response(JSON.stringify({ annotations: MOCK_ANNOTATIONS }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      return new Response("Not Found", { status: 404 });
    });

    render(
      <TestQueryWrapper queryClient={queryClient}>
        <AnnotationsPanel filePath="docs/guide.md" />
      </TestQueryWrapper>,
    );

    await waitFor(() => {
      expect(
        screen.getByText("This section needs a code example."),
      ).toBeInTheDocument();
    });

    expect(
      screen.getByText("Consider adding a warning about rate limits."),
    ).toBeInTheDocument();
    expect(screen.getByText("jane")).toBeInTheDocument();
    expect(screen.getByText("review-bot")).toBeInTheDocument();
  });

  test("creating annotation calls API", async () => {
    const user = userEvent.setup();

    fetchSpy.mockImplementation(async (input) => {
      const url = extractFetchUrl(input);
      if (url.includes("/rpc/corpus.listAnnotations")) {
        return new Response(JSON.stringify({ annotations: MOCK_ANNOTATIONS }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url.includes("/rpc/corpus.createAnnotation")) {
        return new Response(
          JSON.stringify({
            id: "a3",
            author: "current-user",
            authorType: "human",
            content: "New annotation text",
            createdAt: "2026-04-10T08:00:00Z",
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
        <AnnotationsPanel filePath="docs/guide.md" />
      </TestQueryWrapper>,
    );

    await waitFor(() => {
      expect(
        screen.getByText("This section needs a code example."),
      ).toBeInTheDocument();
    });

    const addButton = screen.getByRole("button", { name: /add annotation/i });
    await user.click(addButton);

    const textarea = screen.getByPlaceholderText(/add a note/i);
    await user.type(textarea, "New annotation text");

    const submitButton = screen.getByRole("button", { name: /submit/i });
    await user.click(submitButton);

    await waitFor(() => {
      const calls = fetchSpy.mock.calls.filter((call) =>
        extractFetchUrl(call[0]).includes("/rpc/corpus.createAnnotation"),
      );
      expect(calls).toHaveLength(1);
    });
  });
});
