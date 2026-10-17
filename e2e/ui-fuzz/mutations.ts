import type { ScreenState } from "./questions.ts";

// A mutation rewrites one API response the screen depends on. The pure part
// lives here so it can be unit tested; the spec wires it into
// `page.route`. `expected` is the set of states a correct UI may land in.
// A mutation that cannot be applied to the real payload (for example cloning
// a row when the list is empty) returns `skip` with a reason, so the case is
// recorded as skipped rather than silently measured against the wrong input.

export type MockedResponse = {
  status: number;
  contentType: string;
  body: string;
};

export type Applied =
  | { kind: "respond"; response: MockedResponse }
  | { kind: "skip"; reason: string };

export type Mutation = {
  name: string;
  // Which rpc path this mutation intercepts, compared against URL.pathname.
  rpcPath: string;
  expected: ReadonlySet<ScreenState>;
  // `original` is the real response body the stack returned; undefined when
  // the mutation never needs it (the spec then fulfils without fetching).
  needsOriginal: boolean;
  apply(original: string | undefined): Applied;
};

const json = (status: number, value: unknown): Applied => ({
  kind: "respond",
  response: {
    status,
    contentType: "application/json",
    body: JSON.stringify(value),
  },
});

// A name long enough to overflow any column, with no spaces so the browser
// cannot wrap it. Realistic for a toolset slug pasted from a script.
export const LONG_NAME = "mcp-" + "x".repeat(4996);

type ListBody = { mcp_servers?: unknown[] };

function parseList(original: string | undefined): ListBody | undefined {
  if (original === undefined) return undefined;
  try {
    const v: unknown = JSON.parse(original);
    if (
      typeof v === "object" &&
      v !== null &&
      Array.isArray((v as ListBody).mcp_servers)
    ) {
      return v as ListBody;
    }
  } catch {
    // fall through
  }
  return undefined;
}

export const MCP_LIST_RPC = "/rpc/mcpServers.list";

export const mcpListMutations: Mutation[] = [
  {
    name: "baseline",
    rpcPath: MCP_LIST_RPC,
    // The seed may or may not provision servers; both are correct renders of
    // an untouched response. What baseline guards is `broken`.
    expected: new Set(["loaded", "empty"]),
    needsOriginal: true,
    apply(original) {
      if (original === undefined)
        return { kind: "skip", reason: "no original response" };
      return {
        kind: "respond",
        response: {
          status: 200,
          contentType: "application/json",
          body: original,
        },
      };
    },
  },
  {
    name: "empty",
    rpcPath: MCP_LIST_RPC,
    expected: new Set(["empty"]),
    needsOriginal: false,
    apply: () => json(200, { mcp_servers: [] }),
  },
  {
    name: "server-error",
    rpcPath: MCP_LIST_RPC,
    expected: new Set(["error"]),
    needsOriginal: false,
    apply: () =>
      json(500, {
        name: "unexpected",
        id: "ui-fuzz",
        message: "internal server error",
        temporary: false,
        timeout: false,
        fault: true,
      }),
  },
  {
    name: "long-name",
    rpcPath: MCP_LIST_RPC,
    expected: new Set(["loaded"]),
    needsOriginal: true,
    apply(original) {
      const list = parseList(original);
      const first = list?.mcp_servers?.[0];
      if (!list || typeof first !== "object" || first === null) {
        return { kind: "skip", reason: "seed has no MCP server to clone" };
      }
      const clone = { ...(first as Record<string, unknown>), name: LONG_NAME };
      return json(200, {
        ...list,
        mcp_servers: [clone, ...(list.mcp_servers ?? [])],
      });
    },
  },
];
