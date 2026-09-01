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

function renderActivity({ throwOnError = false } = {}): QueryClient {
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
  return client;
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

  it.each([
    { name: "empty", cachedLogs: [] },
    {
      name: "nonempty",
      cachedLogs: [{ ...baseLog, id: "cached", action: "project:update" }],
    },
  ])(
    "keeps cached $name data visible through a failed refetch and retries successfully",
    async ({ cachedLogs }) => {
      const refreshed = {
        ...baseLog,
        id: "refreshed",
        action: "organization:enterprise_trial_armed",
        metadata: { trial_ends_at: "2026-09-08T12:00:00Z" },
      };
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce(jsonResponse({ logs: cachedLogs }))
        .mockResolvedValueOnce(
          jsonResponse({ message: "private failure" }, 500),
        )
        .mockResolvedValueOnce(jsonResponse({ logs: [refreshed] }));
      vi.stubGlobal("fetch", fetchMock);
      const client = renderActivity();

      if (cachedLogs.length === 0) {
        await screen.findByText("No activity yet.");
      } else {
        await screen.findByTestId("activity-cached");
      }
      await client.invalidateQueries({
        queryKey: ["platform-admin", "organization-activity", "<ORG_ID>"],
      });

      expect(
        await screen.findByText("Activity could not be refreshed."),
      ).toBeDefined();
      if (cachedLogs.length === 0) {
        expect(screen.getByText("No activity yet.")).toBeDefined();
      } else {
        expect(screen.getByTestId("activity-cached")).toBeDefined();
      }
      fireEvent.click(screen.getByRole("button", { name: "Retry refresh" }));

      expect(await screen.findByTestId("activity-refreshed")).toBeDefined();
      expect(screen.queryByText("Activity could not be refreshed.")).toBeNull();
      expect(fetchMock).toHaveBeenCalledTimes(3);
    },
  );
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
