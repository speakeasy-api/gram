import { execFile } from "node:child_process";
import { promisify } from "node:util";

type PRState = "open" | "draft" | "merged" | "closed";
export type PrInfo = { number: number; url: string; state: PRState };
type GitHubPR = {
  number: number;
  url: string;
  headRefName: string;
  state: "OPEN" | "MERGED" | "CLOSED";
  isDraft: boolean;
};

const exec = promisify(execFile);
const queryGitHub = async (args: string[]) =>
  (await exec("gh", args, { maxBuffer: 8 * 1024 * 1024 })).stdout;
const PR_RANK = { open: 0, draft: 0, merged: 1, closed: 2 };
const EMPTY_TTL_MS = 5 * 60_000;

export function createPRLookup(query = queryGitHub, now = Date.now) {
  let previous = new Map<string, PrInfo>();
  const emptyUntil = new Map<string, number>();

  return async (branches: string[]): Promise<Map<string, PrInfo>> => {
    const next = new Map<string, PrInfo>();
    const read = async (state: string, limit: string, branch?: string) => {
      const prs: GitHubPR[] = JSON.parse(
        await query([
          "pr",
          "list",
          "--state",
          state,
          "--json",
          "number,url,headRefName,state,isDraft",
          "--limit",
          limit,
          ...(branch ? ["--head", branch] : []),
        ]),
      );
      for (const pr of prs) {
        const state: PRState =
          pr.state === "OPEN"
            ? pr.isDraft
              ? "draft"
              : "open"
            : pr.state === "MERGED"
              ? "merged"
              : "closed";
        const existing = next.get(pr.headRefName);
        // gh lists newest first, so the first result wins within each state.
        if (existing && PR_RANK[existing.state] <= PR_RANK[state]) continue;
        next.set(pr.headRefName, { number: pr.number, url: pr.url, state });
        emptyUntil.delete(pr.headRefName);
      }
    };

    try {
      // Recent PRs appear immediately, even for branches with a cached miss.
      await read("all", "200");
      const pending = [...new Set(branches)].filter((branch) => {
        if (!branch) return false;
        const pr = next.get(branch);
        return pr
          ? PR_RANK[pr.state] > 0
          : (emptyUntil.get(branch) ?? 0) <= now();
      });
      // At most four gh processes at once; each branch checks states serially.
      for (let i = 0; i < pending.length; i += 4) {
        await Promise.allSettled(
          pending.slice(i, i + 4).map(async (branch) => {
            try {
              for (const state of ["open", "merged", "closed"] as const) {
                const pr = next.get(branch);
                if (pr && PR_RANK[pr.state] <= PR_RANK[state]) break;
                // One result per state avoids truncating away an older preferred
                // PR, including when the bulk window contains a newer closed PR.
                await read(state, "1", branch);
              }
              if (!next.has(branch))
                emptyUntil.set(branch, now() + EMPTY_TTL_MS);
            } catch {
              const cached = previous.get(branch);
              if (cached && !next.has(branch)) next.set(branch, cached);
              // Failed requests aren't evidence that a branch has no PR.
            }
          }),
        );
      }
      previous = next;
    } catch {
      // gh missing or offline: retain the last successful lookup.
    }
    return previous;
  };
}
