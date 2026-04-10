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

function renderWithQuery(ui: React.ReactElement) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>,
  );
}

let fetchSpy: ReturnType<typeof vi.spyOn>;

beforeEach(() => {
  fetchSpy = vi.spyOn(globalThis, "fetch");
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("YoloMode", () => {
  test("renders toggle from config API", async () => {
    const config = makeConfig({ enabled: true });
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify(config), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );

    renderWithQuery(<YoloMode projectId="proj-1" />);

    const toggle = await screen.findByRole("switch");
    expect(toggle).toBeInTheDocument();
    expect(toggle).toHaveAttribute("aria-checked", "true");
  });

  test("toggling calls setAutoPublishConfig", async () => {
    const user = userEvent.setup();

    // Initial fetch: disabled config
    const config = makeConfig({ enabled: false });
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify(config), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );

    renderWithQuery(<YoloMode projectId="proj-1" />);

    const toggle = await screen.findByRole("switch");
    expect(toggle).toHaveAttribute("aria-checked", "false");

    // Mock the PUT response for toggling on
    const updatedConfig = makeConfig({ enabled: true });
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify(updatedConfig), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );

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
    const config = makeConfig({ enabled: true, intervalMinutes: 30 });
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify(config), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );

    renderWithQuery(<YoloMode projectId="proj-1" />);

    // Wait for config to load and panel to appear
    await waitFor(() => {
      expect(screen.getByRole("switch")).toHaveAttribute(
        "aria-checked",
        "true",
      );
    });

    // Interval and filter controls should be visible when enabled
    expect(screen.getByLabelText(/interval/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/minimum upvotes/i)).toBeInTheDocument();
  });
});
