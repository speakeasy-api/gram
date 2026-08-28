import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
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

function renderActivity(): void {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
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
  it("continues past a filtered page before declaring the feed empty", async () => {
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

    expect(await screen.findByText("armed enterprise trial")).toBeDefined();
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      expect.stringContaining("cursor=older-page"),
      expect.anything(),
    );
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
    expect(screen.getByText("armed enterprise trial")).toBeDefined();
    fireEvent.click(
      screen.getByRole("button", { name: "Retry older activity" }),
    );

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3));
    const items = await screen.findAllByTestId(/^activity-/);
    expect(items.map((item) => item.dataset.testid)).toEqual([
      "activity-newer",
      "activity-boundary",
    ]);
    expect(fetchMock.mock.calls.slice(1).map(([url]) => String(url))).toEqual([
      expect.stringContaining("cursor=page-2"),
      expect.stringContaining("cursor=page-2"),
    ]);
  });
});
