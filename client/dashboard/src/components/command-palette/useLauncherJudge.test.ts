import type { LauncherJudgment } from "@gram/client/models/components/launcherjudgment.js";
import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Judgment, LauncherCandidate } from "./candidates/types";
import { useLauncherJudge } from "./useLauncherJudge";

const mocks = vi.hoisted(() => ({
  mutateAsync: vi.fn(),
}));

vi.mock("@gram/client/react-query/launcherJudge.js", () => ({
  useLauncherJudgeMutation: () => ({ mutateAsync: mocks.mutateAsync }),
}));

interface Deferred {
  resolve: (value: LauncherJudgment) => void;
  reject: (reason: unknown) => void;
  signal: AbortSignal | undefined;
  body: unknown;
}

/** Queues every mutateAsync call so tests settle them in any order. */
function captureCalls(): Deferred[] {
  const calls: Deferred[] = [];
  mocks.mutateAsync.mockImplementation(
    (vars: {
      request: { judgeRequestBody: unknown };
      options?: { fetchOptions?: { signal?: AbortSignal } };
    }) =>
      new Promise<LauncherJudgment>((resolve, reject) => {
        calls.push({
          resolve,
          reject,
          signal: vars.options?.fetchOptions?.signal,
          body: vars.request.judgeRequestBody,
        });
      }),
  );
  return calls;
}

function candidate(
  id: string,
  kind: LauncherCandidate["kind"] = "mcp_server",
): LauncherCandidate {
  return {
    id,
    kind,
    title: `Title ${id}`,
    detail: `Detail ${id}`,
    keywords: ["secret-keyword"],
    verbs: ["open"],
    group: "Group",
    run: () => undefined,
  };
}

function judgmentFor(id: string, latencyMs = 40): LauncherJudgment {
  return {
    disabled: false,
    target: { [id]: 0.9, none: 0.1 },
    action: { open: 1 },
    ready: 0.8,
    latencyMs,
  };
}

async function flush() {
  await act(async () => {
    await Promise.resolve();
  });
}

beforeEach(() => {
  mocks.mutateAsync.mockReset();
});

const SCOPE = "acme/default";

describe("useLauncherJudge", () => {
  it("drops a stale response that resolves after a newer one", async () => {
    const calls = captureCalls();
    const { result } = renderHook(() => useLauncherJudge(SCOPE));

    act(() => result.current.judge("a", [candidate("a"), candidate("b")]));
    act(() => result.current.judge("ab", [candidate("a"), candidate("b")]));
    expect(calls).toHaveLength(2);
    expect(result.current.state.inFlight).toBe(true);

    calls[1]?.resolve(judgmentFor("b", 55));
    await flush();
    expect(result.current.state.judgment?.target).toEqual({
      b: 0.9,
      none: 0.1,
    });
    expect(result.current.state.fresh).toBe(true);
    expect(result.current.state.inFlight).toBe(false);
    expect(result.current.state.latencyMs).toBe(55);

    calls[0]?.resolve(judgmentFor("a", 10));
    await flush();
    expect(result.current.state.judgment?.target).toEqual({
      b: 0.9,
      none: 0.1,
    });
    expect(result.current.state.latencyMs).toBe(55);
  });

  it("aborts the previous request and keeps its answer from applying", async () => {
    const calls = captureCalls();
    const { result } = renderHook(() => useLauncherJudge(SCOPE));

    act(() => result.current.judge("a", [candidate("a")]));
    expect(calls[0]?.signal?.aborted).toBe(false);

    act(() => result.current.judge("ab", [candidate("a")]));
    expect(calls[0]?.signal?.aborted).toBe(true);
    expect(calls[1]?.signal?.aborted).toBe(false);

    // The aborted request rejects while the newer request is in flight: its
    // failure must not clear the state the newer request is working with.
    calls[0]?.reject(new Error("aborted"));
    await flush();
    expect(result.current.state.inFlight).toBe(true);
    expect(result.current.state.judgment).toBeNull();

    calls[1]?.resolve(judgmentFor("a"));
    await flush();
    expect(result.current.state.judgment?.target).toEqual({
      a: 0.9,
      none: 0.1,
    });
    expect(result.current.state.fresh).toBe(true);
  });

  it("keeps the previous judgment, dimmed, while a newer request is in flight", async () => {
    const calls = captureCalls();
    const { result } = renderHook(() => useLauncherJudge(SCOPE));

    act(() => result.current.judge("a", [candidate("a")]));
    calls[0]?.resolve(judgmentFor("a"));
    await flush();
    expect(result.current.state.fresh).toBe(true);

    act(() => result.current.judge("ab", [candidate("a")]));
    expect(result.current.state.judgment?.target).toEqual({
      a: 0.9,
      none: 0.1,
    });
    expect(result.current.state.fresh).toBe(false);
    expect(result.current.state.inFlight).toBe(true);
  });

  it("latches disabled and stops calling the mutation", async () => {
    const calls = captureCalls();
    const { result } = renderHook(() => useLauncherJudge(SCOPE));

    act(() => result.current.judge("a", [candidate("a")]));
    calls[0]?.resolve({ disabled: true });
    await flush();
    expect(result.current.state.disabled).toBe(true);
    expect(result.current.state.judgment).toBeNull();
    expect(result.current.state.inFlight).toBe(false);

    act(() => result.current.judge("ab", [candidate("a")]));
    expect(mocks.mutateAsync).toHaveBeenCalledTimes(1);

    act(() => result.current.reset());
    expect(result.current.state.disabled).toBe(true);
  });

  it("clears the judgment on error without throwing", async () => {
    const calls = captureCalls();
    const { result } = renderHook(() => useLauncherJudge(SCOPE));

    act(() => result.current.judge("a", [candidate("a")]));
    calls[0]?.resolve(judgmentFor("a"));
    await flush();
    expect(result.current.state.judgment).not.toBeNull();

    act(() => result.current.judge("ab", [candidate("a")]));
    calls[1]?.reject(new Error("upstream 502"));
    await flush();
    expect(result.current.state.judgment).toBeNull();
    expect(result.current.state.fresh).toBe(false);
    expect(result.current.state.inFlight).toBe(false);
    expect(result.current.state.latencyMs).toBeNull();
    expect(result.current.state.disabled).toBe(false);

    // The next keystroke retries.
    act(() => result.current.judge("abc", [candidate("a")]));
    expect(mocks.mutateAsync).toHaveBeenCalledTimes(3);
  });

  it("strips person candidates and keywords from the request body", () => {
    const calls = captureCalls();
    const { result } = renderHook(() => useLauncherJudge(SCOPE));

    act(() =>
      result.current.judge("al", [
        candidate("mcp:1"),
        candidate("person:alice", "person"),
      ]),
    );

    expect(calls).toHaveLength(1);
    expect(calls[0]?.body).toEqual({
      query: "al",
      candidates: [
        {
          id: "mcp:1",
          kind: "mcp_server",
          title: "Title mcp:1",
          detail: "Detail mcp:1",
          verbs: ["open"],
        },
      ],
    });
  });

  it("forwards the current route as context for tie-breaks", () => {
    const calls = captureCalls();
    const { result } = renderHook(() => useLauncherJudge(SCOPE));

    act(() =>
      result.current.judge("sett", [candidate("page:/settings")], "/acme/mcp"),
    );

    expect(calls).toHaveLength(1);
    const body = calls[0]?.body as { context?: { route?: string } };
    expect(body.context).toEqual({ route: "/acme/mcp" });
  });

  it("never sends fuzzy-only candidates such as identity-page recents", () => {
    const calls = captureCalls();
    const { result } = renderHook(() => useLauncherJudge(SCOPE));

    const identityRecent: LauncherCandidate = {
      ...candidate(
        "recent:/acme/projects/default/identities/dXNlcjox/overview",
        "recent",
      ),
      title: "ada@example.test",
      fuzzyOnly: true,
    };
    act(() =>
      result.current.judge("ada", [candidate("mcp:1"), identityRecent]),
    );

    expect(calls).toHaveLength(1);
    const body = calls[0]?.body as { candidates: Array<{ id: string }> };
    expect(body.candidates.map((c) => c.id)).toEqual(["mcp:1"]);
    expect(JSON.stringify(body)).not.toContain("ada@example.test");
  });

  it("clamps the query, titles and details to the limits the server enforces", () => {
    const calls = captureCalls();
    const { result } = renderHook(() => useLauncherJudge(SCOPE));

    const long: LauncherCandidate = {
      ...candidate("access_request:1", "access_request"),
      title: "t".repeat(200),
      detail: "d".repeat(300),
    };
    // Astral characters count as one rune server-side but two UTF-16 units;
    // the clamp must count code points so a surrogate pair is never split.
    const emoji: LauncherCandidate = {
      ...candidate("mcp:emoji"),
      title: "😀".repeat(130),
    };
    act(() => result.current.judge("q".repeat(250), [long, emoji]));

    expect(calls).toHaveLength(1);
    const body = calls[0]?.body as {
      query: string;
      candidates: Array<{ title: string; detail?: string }>;
    };
    // Goa: query MaxLength(200), title MaxLength(120), detail MaxLength(160).
    expect(body.query).toBe("q".repeat(200));
    expect(body.candidates[0]?.title).toBe("t".repeat(120));
    expect(body.candidates[0]?.detail).toBe("d".repeat(160));
    expect(body.candidates[1]?.title).toBe("😀".repeat(120));
  });

  it("skips the call and clears the judgment when nothing is worth sending", async () => {
    const calls = captureCalls();
    const { result } = renderHook(() => useLauncherJudge(SCOPE));

    act(() => result.current.judge("a", [candidate("a")]));
    calls[0]?.resolve(judgmentFor("a"));
    await flush();
    expect(result.current.state.judgment).not.toBeNull();

    act(() => result.current.judge("", [candidate("a")]));
    expect(result.current.state.judgment).toBeNull();
    expect(result.current.state.inFlight).toBe(false);

    act(() =>
      result.current.judge("al", [candidate("person:alice", "person")]),
    );
    expect(result.current.state.judgment).toBeNull();
    expect(mocks.mutateAsync).toHaveBeenCalledTimes(1);
  });

  // The intent service is provisioned per tenant: a latch taken in an
  // organization without one must not follow the user into one that has it.
  it("drops the disabled latch when the scope changes and asks again", async () => {
    const calls = captureCalls();
    const { result, rerender } = renderHook(
      ({ scope }: { scope: string }) => useLauncherJudge(scope),
      { initialProps: { scope: "no-key-org/default" } },
    );

    act(() => result.current.judge("a", [candidate("a")]));
    calls[0]?.resolve({ disabled: true });
    await flush();
    expect(result.current.state.disabled).toBe(true);

    rerender({ scope: "keyed-org/default" });
    expect(result.current.state.disabled).toBe(false);

    act(() => result.current.judge("a", [candidate("a")]));
    expect(mocks.mutateAsync).toHaveBeenCalledTimes(2);
    calls[1]?.resolve(judgmentFor("a"));
    await flush();
    expect(result.current.state.judgment?.target).toEqual({
      a: 0.9,
      none: 0.1,
    });

    // Returning to the tenant without a service asks once more and latches
    // again there, rather than remembering the earlier answer.
    rerender({ scope: "no-key-org/default" });
    expect(result.current.state.disabled).toBe(false);
    expect(result.current.state.judgment).toBeNull();
    act(() => result.current.judge("a", [candidate("a")]));
    expect(mocks.mutateAsync).toHaveBeenCalledTimes(3);
  });

  // The reset effect runs after paint. The very first render for the new
  // tenant must already be clean, or the palette ranks its rows and picks
  // its highlight by the previous tenant's judgment for one frame.
  it("shows no judgment on the very first render after the scope changes", async () => {
    const calls = captureCalls();
    const seen: Array<Judgment | null> = [];
    const { result, rerender } = renderHook(
      ({ scope }: { scope: string }) => {
        const hook = useLauncherJudge(scope);
        seen.push(hook.state.judgment);
        return hook;
      },
      { initialProps: { scope: "acme/one" } },
    );

    act(() => result.current.judge("a", [candidate("a")]));
    calls[0]?.resolve(judgmentFor("a"));
    await flush();
    expect(result.current.state.judgment).not.toBeNull();

    const before = seen.length;
    rerender({ scope: "acme/two" });
    const after = seen.slice(before);
    expect(after.length).toBeGreaterThan(0);
    expect(after.filter((judgment) => judgment !== null)).toEqual([]);
  });

  it("keeps the latch while the scope is unchanged across re-renders", async () => {
    const calls = captureCalls();
    const { result, rerender } = renderHook(
      ({ scope }: { scope: string }) => useLauncherJudge(scope),
      { initialProps: { scope: SCOPE } },
    );

    act(() => result.current.judge("a", [candidate("a")]));
    calls[0]?.resolve({ disabled: true });
    await flush();

    rerender({ scope: SCOPE });
    expect(result.current.state.disabled).toBe(true);
    act(() => result.current.judge("ab", [candidate("a")]));
    expect(mocks.mutateAsync).toHaveBeenCalledTimes(1);
  });

  it("lets no request from the previous scope land after the scope changes", async () => {
    const calls = captureCalls();
    const { result, rerender } = renderHook(
      ({ scope }: { scope: string }) => useLauncherJudge(scope),
      { initialProps: { scope: "acme/one" } },
    );

    act(() => result.current.judge("a", [candidate("a")]));
    rerender({ scope: "acme/two" });
    expect(calls[0]?.signal?.aborted).toBe(true);
    expect(result.current.state.inFlight).toBe(false);

    calls[0]?.resolve(judgmentFor("a"));
    await flush();
    expect(result.current.state.judgment).toBeNull();
  });

  // Dismissing the palette must not let a slow answer land afterwards.
  it("reset aborts the request in flight and its late answer never lands", async () => {
    const calls = captureCalls();
    const { result } = renderHook(() => useLauncherJudge(SCOPE));

    act(() => result.current.judge("a", [candidate("a")]));
    expect(result.current.state.inFlight).toBe(true);

    act(() => result.current.reset());
    expect(calls[0]?.signal?.aborted).toBe(true);
    expect(result.current.state.inFlight).toBe(false);

    calls[0]?.resolve(judgmentFor("a"));
    await flush();
    expect(result.current.state.judgment).toBeNull();
    expect(result.current.state.fresh).toBe(false);
    expect(result.current.state.latencyMs).toBeNull();

    // A rejection of the same stale request is just as inert.
    act(() => result.current.judge("b", [candidate("b")]));
    act(() => result.current.reset());
    calls[1]?.reject(new Error("aborted"));
    await flush();
    expect(result.current.state.judgment).toBeNull();
    expect(result.current.state.inFlight).toBe(false);
  });

  it("keeps the newer judgment when an older request rejects after it applied", async () => {
    const calls = captureCalls();
    const { result } = renderHook(() => useLauncherJudge(SCOPE));

    act(() => result.current.judge("a", [candidate("a"), candidate("b")]));
    act(() => result.current.judge("ab", [candidate("a"), candidate("b")]));
    calls[1]?.resolve(judgmentFor("b", 55));
    await flush();
    expect(result.current.state.judgment?.target).toEqual({
      b: 0.9,
      none: 0.1,
    });

    calls[0]?.reject(new Error("aborted"));
    await flush();
    expect(result.current.state.judgment?.target).toEqual({
      b: 0.9,
      none: 0.1,
    });
    expect(result.current.state.fresh).toBe(true);
    expect(result.current.state.latencyMs).toBe(55);
  });

  it("reset clears the judgment and latency", async () => {
    const calls = captureCalls();
    const { result } = renderHook(() => useLauncherJudge(SCOPE));

    act(() => result.current.judge("a", [candidate("a")]));
    calls[0]?.resolve(judgmentFor("a"));
    await flush();

    act(() => result.current.reset());
    expect(result.current.state.judgment).toBeNull();
    expect(result.current.state.latencyMs).toBeNull();
    expect(result.current.state.fresh).toBe(false);
    expect(result.current.state.disabled).toBe(false);
  });
});
