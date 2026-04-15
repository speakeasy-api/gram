import { describe, test, expect, vi, beforeEach, afterEach } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  createTestQueryClient,
  extractFetchUrl,
  TestQueryWrapper,
} from "@/test-utils";
import { YoloMode } from "./YoloMode";

function jsonResponse(data: unknown): Response {
  return new Response(JSON.stringify(data), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

describe("YoloMode", () => {
  let fetchSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    fetchSpy = vi.spyOn(globalThis, "fetch");
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  test("renders toggle from config API", async () => {
    fetchSpy.mockResolvedValueOnce(
      jsonResponse({
        enabled: true,
        interval_minutes: 10,
        min_upvotes: 0,
        min_age_hours: 0,
      }),
    );

    const queryClient = createTestQueryClient();

    render(
      <TestQueryWrapper queryClient={queryClient}>
        <YoloMode projectId="proj-1" />
      </TestQueryWrapper>,
    );

    const toggle = await screen.findByRole("switch");
    expect(toggle).toBeInTheDocument();
    expect(toggle).toHaveAttribute("aria-checked", "true");
  });

  test("toggling calls setAutoPublishConfig", async () => {
    const user = userEvent.setup();
    const requestBodies = new Map<string, Record<string, unknown>>();

    fetchSpy.mockImplementation(async (input: RequestInfo | URL) => {
      const url = extractFetchUrl(input);
      if (input instanceof Request && input.body) {
        requestBodies.set(
          url,
          (await input.clone().json()) as Record<string, unknown>,
        );
      }

      if (url.includes("/rpc/corpus.getAutoPublishConfig")) {
        return jsonResponse({
          enabled: false,
          interval_minutes: 10,
          min_upvotes: 0,
          min_age_hours: 0,
        });
      }

      if (url.includes("/rpc/corpus.setAutoPublishConfig")) {
        return jsonResponse({
          enabled: true,
          interval_minutes: 10,
          min_upvotes: 0,
          min_age_hours: 0,
        });
      }

      return new Response("Not Found", { status: 404 });
    });

    const queryClient = createTestQueryClient();

    render(
      <TestQueryWrapper queryClient={queryClient}>
        <YoloMode projectId="proj-1" />
      </TestQueryWrapper>,
    );

    const toggle = await screen.findByRole("switch");
    await waitFor(() => {
      expect(toggle).toHaveAttribute("aria-checked", "false");
    });

    await user.click(toggle);

    await waitFor(() => {
      const updateCall = fetchSpy.mock.calls.find((call: [RequestInfo | URL]) =>
        extractFetchUrl(call[0]).includes("/rpc/corpus.setAutoPublishConfig"),
      );
      expect(updateCall).toBeDefined();
      expect(requestBodies.get(extractFetchUrl(updateCall?.[0]))).toEqual({
        enabled: true,
        interval_minutes: 10,
        min_upvotes: 0,
        min_age_hours: 0,
      });
    });
  });

  test("shows config panel when enabled", async () => {
    fetchSpy.mockResolvedValueOnce(
      jsonResponse({
        enabled: true,
        interval_minutes: 30,
        min_upvotes: 0,
        min_age_hours: 0,
      }),
    );

    const queryClient = createTestQueryClient();

    render(
      <TestQueryWrapper queryClient={queryClient}>
        <YoloMode projectId="proj-1" />
      </TestQueryWrapper>,
    );

    await waitFor(() => {
      expect(screen.getByRole("switch")).toHaveAttribute(
        "aria-checked",
        "true",
      );
    });

    expect(screen.getByLabelText(/interval/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/minimum upvotes/i)).toBeInTheDocument();
  });
});
