import { cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useGitHubAccountSearch } from "./useGitHubAccountSearch";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it("preserves collaborator search and limits setup results to organizations", async () => {
  const items = [
    { login: "example-person", type: "User" },
    { login: "example-org", type: "Organization" },
  ];
  const fetchAccounts = vi.fn(async (_url: string) => Response.json({ items }));
  vi.stubGlobal("fetch", fetchAccounts);

  const { result, rerender } = renderHook(
    (organizationsOnly: boolean) =>
      useGitHubAccountSearch("example", organizationsOnly),
    { initialProps: false },
  );
  await waitFor(() => expect(result.current.results).toEqual(items));
  expect(new URL(fetchAccounts.mock.calls[0]![0]).searchParams.get("q")).toBe(
    "example",
  );

  rerender(true);
  await waitFor(() => expect(result.current.results).toEqual([items[1]]));
  expect(new URL(fetchAccounts.mock.calls[1]![0]).searchParams.get("q")).toBe(
    "example in:login type:org",
  );
});
