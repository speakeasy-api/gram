import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { ErrorBoundary } from "react-error-boundary";
import { afterEach, describe, expect, it, vi } from "vitest";
import { OrganizationActivity } from "./Activity";

const baseLog = {
  acting_surface: "unknown",
  actor_id: "system",
  actor_type: "user",
  created_at: "2026-08-25T12:00:00Z",
  subject_id: "<ORG_ID>",
  subject_type: "organization",
};

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

function renderActivity({ throwOnError = false } = {}): void {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, throwOnError } },
  });
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>
      <ErrorBoundary fallback={<p>Error loading Page</p>}>
        {children}
      </ErrorBoundary>
    </QueryClientProvider>
  );
  render(<OrganizationActivity organizationId="<ORG_ID>" />, {
    wrapper: Wrapper,
  });
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("organization activity pagination query", () => {
  it("keeps unrelated activity and loads the next page in place", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        jsonResponse({
          logs: [{ ...baseLog, id: "other", action: "project:update" }],
          next_cursor: "older-page",
        }),
      )
      .mockResolvedValueOnce(
        jsonResponse({
          logs: [
            {
              ...baseLog,
              id: "trial",
              action: "organization:enterprise_trial_armed",
            },
          ],
        }),
      );
    vi.stubGlobal("fetch", fetchMock);

    renderActivity();
    const loadOlder = await screen.findByRole("button", {
      name: "Load older activity",
    });
    expect(screen.queryByText("No activity yet.")).toBeNull();
    fireEvent.click(loadOlder);

    expect(await screen.findByText("started enterprise trial")).toBeDefined();
    expect(
      screen.getAllByTestId(/^activity-/).map((item) => item.dataset.testid),
    ).toEqual(["activity-other", "activity-trial"]);
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      expect.stringContaining("cursor=older-page"),
      expect.anything(),
    );
  });

  it("keeps a filtered feed mounted when loading older activity fails under route throw semantics", async () => {
    const unhandledRejection = vi.fn((event: PromiseRejectionEvent) => {
      event.preventDefault();
    });
    window.addEventListener("unhandledrejection", unhandledRejection);
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        jsonResponse({
          logs: [{ ...baseLog, id: "other", action: "project:update" }],
          next_cursor: "older",
        }),
      )
      .mockResolvedValueOnce(jsonResponse({ message: "private failure" }, 500))
      .mockResolvedValueOnce(
        jsonResponse({
          logs: [
            {
              ...baseLog,
              id: "trial",
              action: "organization:enterprise_trial_extended",
            },
          ],
        }),
      );
    vi.stubGlobal("fetch", fetchMock);

    renderActivity({ throwOnError: true });
    fireEvent.click(
      await screen.findByRole("button", { name: "Load older activity" }),
    );

    expect(
      await screen.findByRole("alert", {
        name: "Older activity could not be loaded.",
      }),
    ).toBeDefined();
    expect(screen.queryByText("Error loading Page")).toBeNull();
    expect(screen.queryByText("No activity yet.")).toBeNull();

    fireEvent.click(
      screen.getByRole("button", { name: "Retry older activity" }),
    );
    expect(await screen.findByText("extended enterprise trial")).toBeDefined();
    expect(screen.queryByText("Error loading Page")).toBeNull();
    expect(fetchMock.mock.calls.slice(1).map(([url]) => String(url))).toEqual([
      expect.stringContaining("cursor=older"),
      expect.stringContaining("cursor=older"),
    ]);
    expect(unhandledRejection).not.toHaveBeenCalled();
    window.removeEventListener("unhandledrejection", unhandledRejection);
  });

  it("guards rapid clicks and retries a rejected page without losing or duplicating rows", async () => {
    let resolveRejectedPage!: (response: Response) => void;
    const rejectedPage = new Promise<Response>((resolve) => {
      resolveRejectedPage = resolve;
    });
    const boundary = {
      ...baseLog,
      id: "boundary",
      action: "organization:enterprise_trial_armed",
      created_at: "2026-08-24T12:00:00Z",
    };
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        jsonResponse({ logs: [boundary], next_cursor: "page-2" }),
      )
      .mockImplementationOnce(() => rejectedPage)
      .mockResolvedValueOnce(
        jsonResponse({
          logs: [
            boundary,
            {
              ...baseLog,
              id: "newer",
              action: "organization:enterprise_trial_demoted",
              created_at: "2026-08-26T12:00:00Z",
            },
          ],
        }),
      );
    vi.stubGlobal("fetch", fetchMock);

    renderActivity();
    const loadOlder = await screen.findByRole("button", {
      name: "Load older activity",
    });
    fireEvent.click(loadOlder);
    fireEvent.click(loadOlder);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    resolveRejectedPage(jsonResponse({ message: "private failure" }, 500));

    expect(
      await screen.findByRole("alert", {
        name: "Older activity could not be loaded.",
      }),
    ).toBeDefined();
    expect(screen.getByText("started enterprise trial")).toBeDefined();
    fireEvent.click(
      screen.getByRole("button", { name: "Retry older activity" }),
    );

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3));
    const items = await screen.findAllByTestId(/^activity-/);
    expect(items.map((item) => item.dataset.testid)).toEqual([
      "activity-boundary",
      "activity-newer",
    ]);
    expect(fetchMock.mock.calls.slice(1).map(([url]) => String(url))).toEqual([
      expect.stringContaining("cursor=page-2"),
      expect.stringContaining("cursor=page-2"),
    ]);
  });
});
