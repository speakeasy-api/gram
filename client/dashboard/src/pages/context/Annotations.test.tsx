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
    const requestBodies = new Map<string, Record<string, unknown>>();

    fetchSpy.mockImplementation(async (input: RequestInfo | URL) => {
      const url = extractFetchUrl(input);
      if (input instanceof Request) {
        requestBodies.set(
          url,
          (await input.clone().json()) as Record<string, unknown>,
        );
      }
      if (url.includes("/rpc/corpus.listAnnotations")) {
        return new Response(
          JSON.stringify({
            annotations: MOCK_ANNOTATIONS.map((annotation) => ({
              author: annotation.author,
              author_type: annotation.authorType,
              content: annotation.content,
              created_at: annotation.createdAt,
              id: annotation.id,
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

    const listRequest = fetchSpy.mock.calls.find((call: [RequestInfo | URL]) =>
      extractFetchUrl(call[0]).includes("/rpc/corpus.listAnnotations"),
    );

    expect(listRequest).toBeDefined();
    expect(requestBodies.get(extractFetchUrl(listRequest?.[0]))).toEqual({
      file_path: "docs/guide.md",
    });
  });

  test("creating annotation calls API", async () => {
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
      if (url.includes("/rpc/corpus.listAnnotations")) {
        return new Response(
          JSON.stringify({
            annotations: MOCK_ANNOTATIONS.map((annotation) => ({
              author: annotation.author,
              author_type: annotation.authorType,
              content: annotation.content,
              created_at: annotation.createdAt,
              id: annotation.id,
            })),
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }
      if (url.includes("/rpc/corpus.createAnnotation")) {
        return new Response(
          JSON.stringify({
            id: "a3",
            author: "current-user",
            author_type: "human",
            content: "New annotation text",
            created_at: "2026-04-10T08:00:00Z",
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
      const calls = fetchSpy.mock.calls.filter((call: [RequestInfo | URL]) =>
        extractFetchUrl(call[0]).includes("/rpc/corpus.createAnnotation"),
      );
      expect(calls).toHaveLength(1);
    });

    const createRequest = fetchSpy.mock.calls.find(
      (call: [RequestInfo | URL]) =>
        extractFetchUrl(call[0]).includes("/rpc/corpus.createAnnotation"),
    );

    expect(createRequest).toBeDefined();
    expect(requestBodies.get(extractFetchUrl(createRequest?.[0]))).toEqual({
      content: "New annotation text",
      file_path: "docs/guide.md",
    });
  });

  test("falls back to an empty list on permission denied", async () => {
    const user = userEvent.setup();

    fetchSpy.mockImplementation(async (input: RequestInfo | URL) => {
      const url = extractFetchUrl(input);

      if (url.includes("/rpc/corpus.listAnnotations")) {
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

      if (url.includes("/rpc/corpus.createAnnotation")) {
        throw new Error(
          "createAnnotation should not be called when annotation fallback is active",
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
      expect(screen.getByText("Annotations (0)")).toBeInTheDocument();
    });

    const addButton = screen
      .getAllByRole("button", { name: /add annotation/i })
      .find((button) => button.hasAttribute("disabled"));

    expect(addButton).toBeDisabled();

    await user.click(addButton!);

    const createCalls = fetchSpy.mock.calls.filter(
      (call: [RequestInfo | URL]) =>
        extractFetchUrl(call[0]).includes("/rpc/corpus.createAnnotation"),
    );

    expect(createCalls).toHaveLength(0);
  });
});
