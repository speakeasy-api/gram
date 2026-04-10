import { describe, test, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { YoloMode } from "./YoloMode";
import type { AutoPublishConfig } from "@/hooks/useAutoPublish";

function makeConfig(
  overrides: Partial<AutoPublishConfig> = {},
): AutoPublishConfig {
  return {
    enabled: false,
    intervalMinutes: 10,
    minUpvotes: 0,
    authorTypeFilter: null,
    labelFilter: null,
    minAgeHours: 0,
    ...overrides,
  };
}

function jsonResponse(data: unknown): Response {
  return new Response(JSON.stringify(data), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

function renderWithQuery(ui: React.ReactElement) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>,
  );
}

describe("YoloMode", () => {
  let fetchSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    fetchSpy = vi.spyOn(globalThis, "fetch");
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  test("renders toggle from config API", async () => {
    fetchSpy.mockResolvedValueOnce(jsonResponse(makeConfig({ enabled: true })));

    renderWithQuery(<YoloMode projectId="proj-1" />);

    const toggle = await screen.findByRole("switch");
    expect(toggle).toBeInTheDocument();
    expect(toggle).toHaveAttribute("aria-checked", "true");
  });

  test("toggling calls setAutoPublishConfig", async () => {
    const user = userEvent.setup();

    fetchSpy.mockResolvedValueOnce(
      jsonResponse(makeConfig({ enabled: false })),
    );

    renderWithQuery(<YoloMode projectId="proj-1" />);

    const toggle = await screen.findByRole("switch");
    expect(toggle).toHaveAttribute("aria-checked", "false");

    fetchSpy.mockResolvedValueOnce(jsonResponse(makeConfig({ enabled: true })));

    await user.click(toggle);

    await waitFor(() => {
      const putCall = fetchSpy.mock.calls.find(
        (call) => typeof call[1] === "object" && call[1]?.method === "PUT",
      );
      expect(putCall).toBeDefined();
      const body = JSON.parse(putCall![1]!.body as string);
      expect(body.enabled).toBe(true);
    });
  });

  test("shows config panel when enabled", async () => {
    fetchSpy.mockResolvedValueOnce(
      jsonResponse(makeConfig({ enabled: true, intervalMinutes: 30 })),
    );

    renderWithQuery(<YoloMode projectId="proj-1" />);

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
