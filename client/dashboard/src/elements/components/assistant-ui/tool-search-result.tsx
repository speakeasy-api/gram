import { cn } from "@/lib/utils";
import {
  useAui,
  useAuiState,
  type ToolCallMessagePartComponent,
} from "@assistant-ui/react";
import { PlusIcon } from "lucide-react";
import { useMemo } from "react";

import { ToolFallback } from "@/elements/components/assistant-ui/tool-fallback";
import {
  asRecord,
  asString,
  isCatalogBrowseSearch,
} from "@/elements/components/assistant-ui/tool-search-result.helpers";
import { useIsCollapsedToolRun } from "@/elements/contexts/CollapsedToolRunContext";
import { useElements } from "@/elements/hooks/useElements";
import { appendToken } from "@/elements/lib/tool-mentions";

/**
 * The runner's `tool_search` result: full schemas for the query's matches, a
 * name index of the whole catalog, and the live status of every attached MCP
 * server. See `agents/runner/src/tools/tool_search.rs`.
 */
interface ToolSearchPayload {
  servers: ServerStatus[];
  /** Tool name -> one-line description, from the catalog index. */
  briefs: Map<string, string>;
}

interface ServerStatus {
  id: string;
  tools: string[];
}

interface RowTool {
  /** Catalog name, as the model must call it. */
  name: string;
  /** Name with the server namespace stripped, for display. */
  label: string;
}

interface ToolRow {
  key: string;
  category: string;
  /** Server the row's tools came from; shown only when more than one is attached. */
  server: string;
  tools: RowTool[];
}

/**
 * The payload arrives either as the structured object or as a text content
 * item holding that same JSON — which one depends on whether the transport
 * preserved structured content, so both are unwrapped rather than assumed.
 */
function extractPayload(result: unknown): ToolSearchPayload | null {
  // The runner hands the structured body back as a JSON string.
  if (typeof result === "string") {
    try {
      return extractPayload(JSON.parse(result) as unknown);
    } catch {
      return null;
    }
  }

  const record = asRecord(result);
  if (!record) return null;

  const content = record["content"];
  // Live turns deliver the body as `{content: "<json>"}`; a reloaded
  // transcript delivers the same JSON as a bare string.
  if (typeof content === "string") {
    try {
      const payload = extractPayload(JSON.parse(content) as unknown);
      if (payload) return payload;
    } catch {
      // Not the JSON payload — fall through to the structured shapes.
    }
  }
  if (Array.isArray(content)) {
    for (const item of content) {
      const text = asString(asRecord(item)?.["text"]);
      if (!text) continue;
      try {
        const payload = extractPayload(JSON.parse(text) as unknown);
        if (payload) return payload;
      } catch {
        // Not the JSON payload — keep looking through the content items.
      }
    }
  }

  const rawServers = record["servers"];
  if (!Array.isArray(rawServers)) return null;

  // Only servers that answered the handshake carry tools. One that is
  // unavailable or still awaiting authorization has nothing to browse, so it
  // is left out rather than rendered as an empty or disabled group.
  const servers: ServerStatus[] = [];
  for (const item of rawServers) {
    const entry = asRecord(item);
    const id = asString(entry?.["id"]);
    const tools = entry?.["tools"];
    if (!id || !Array.isArray(tools)) continue;
    const names = tools.filter(
      (name): name is string => typeof name === "string",
    );
    if (names.length > 0) servers.push({ id, tools: names });
  }
  if (servers.length === 0) return null;

  const briefs = new Map<string, string>();
  const catalog = record["catalog"];
  if (Array.isArray(catalog)) {
    for (const item of catalog) {
      const entry = asRecord(item);
      const name = asString(entry?.["name"]);
      const brief = asString(entry?.["brief"]);
      if (name && brief) briefs.set(name, brief);
    }
  }

  return { servers, briefs };
}

/**
 * Attached servers are named by slug (`_p-assistants`); drop the marker prefix
 * Gram's own managed servers carry and read the rest as words.
 */
function humanizeServer(id: string): string {
  return id.replace(/^_p-/, "").replace(/[-_]+/g, " ").trim() || id;
}

/**
 * Catalog names are namespaced by the server that serves them
 * (`mcp__p-assistants_platform_memory_recall`). The row already says where a
 * tool came from, so the namespace only pushes the part that distinguishes one
 * tool from the next past the truncation point.
 */
function shortToolName(serverID: string, name: string): string {
  const prefix = `mcp__${serverID.replace(/^_/, "")}_`;
  return name.startsWith(prefix) ? name.slice(prefix.length) : name;
}

/**
 * Subject each tool belongs to, matched against its name in order — the first
 * hit wins, so the more specific subject is listed first (`..._mcp_feedback`
 * is feedback, not MCP; `list_project_mcps` is MCP, not projects).
 *
 * Read from the name because that is all the catalog carries: `tool_search`
 * returns names and one-line briefs, with no subject of its own. A tool whose
 * name says nothing recognizable lands in "Other" rather than inventing a
 * category of one.
 */
const TOOL_CATEGORIES: Array<[RegExp, string]> = [
  [/feedback/, "Feedback"],
  [/doc/, "Docs"],
  [/skill/, "Skills"],
  [/memor/, "Memory"],
  [/trigger/, "Triggers"],
  [/risk/, "Risk"],
  [/mcp|catalog/, "MCP"],
  [/setup|handoff|onboard/, "Setup"],
  [/plugin/, "Plugins"],
  [/deployment/, "Deployments"],
  [/chat/, "Chats"],
  [/log|telemetry|metric|attribute|usage|observability|user/, "Observability"],
  [/project|context/, "Platform"],
];

function categoryOf(name: string): string {
  for (const [pattern, category] of TOOL_CATEGORIES) {
    if (pattern.test(name)) return category;
  }
  return "Other";
}

/** Largest rows first, so the busiest subject reads before the long tail. */
function buildRows(servers: ServerStatus[]): ToolRow[] {
  const multipleServers = servers.length > 1;
  const rows = new Map<string, ToolRow>();
  for (const server of servers) {
    for (const tool of server.tools) {
      const label = shortToolName(server.id, tool);
      const category = categoryOf(label);
      const key = multipleServers ? `${server.id}|${category}` : category;
      const entry = { name: tool, label };
      const existing = rows.get(key);
      if (existing) {
        existing.tools.push(entry);
      } else {
        rows.set(key, {
          key,
          category,
          server: multipleServers ? humanizeServer(server.id) : "",
          tools: [entry],
        });
      }
    }
  }
  return [...rows.values()].sort(
    (a, b) =>
      b.tools.length - a.tools.length || a.category.localeCompare(b.category),
  );
}

/**
 * Renders a `tool_search` result as a browsable catalog of the tools attached
 * to this session, grouped by the MCP server serving each one. Clicking a tool
 * runs it: the mention is sent as a turn of its own, so the search doubles as
 * a launcher.
 *
 * Only a browse search draws the card (see `isCatalogBrowseSearch`), and only
 * where the card can render outside its run's collapsible; anything else
 * renders the generic tool row instead.
 *
 * It goes through the model rather than calling the tool directly because the
 * tools live in the assistant's runtime, not in the page — and most of them
 * take arguments the card has no way to fill. The model reads the mention,
 * works out the arguments, and calls it.
 */
export const ToolSearchResult: ToolCallMessagePartComponent = (props) => {
  const { status, result, args, toolCallId } = props;
  const aui = useAui();
  const { config } = useElements();
  const isCollapsedRun = useIsCollapsedToolRun();
  const composerText = useAuiState(({ thread }) => thread.composer.text);

  // A model often searches several times before it answers, and every search
  // carries the same whole-catalog view — so only the last browse in the
  // message draws a card. Rendering each would stack identical copies.
  // Discovery searches are skipped rather than counted: a turn that discovers
  // and then browses must still draw the browse.
  const isLastBrowse = useAuiState(({ message }) => {
    let last: string | undefined;
    for (const part of message.parts) {
      if (
        part.type === "tool-call" &&
        part.toolName === "tool_search" &&
        isCatalogBrowseSearch(part.args)
      ) {
        last = part.toolCallId;
      }
    }
    return last === toolCallId;
  });

  const payload = useMemo(
    () =>
      status.type === "complete" && isCatalogBrowseSearch(args)
        ? extractPayload(result)
        : null,
    [status.type, result, args],
  );

  // Anything this component can't read — a discovery search, a run still
  // streaming, a denied call, an error payload, a search that reached no
  // connected server — belongs to the generic tool card, which already renders
  // those states. A host that replaced that card keeps it here: declining is
  // the common path now that discovery searches take it.
  //
  // A browse that could not be hoisted out of its run declines too. The
  // catalog is an answer, and an answer folded into a disclosure is worse than
  // no card at all — inside the run this is one more call among the mechanics.
  const Fallback = config.components?.ToolFallback ?? ToolFallback;
  if (!payload || isCollapsedRun) {
    return <Fallback {...props} />;
  }
  if (!isLastBrowse) return null;

  const rows = buildRows(payload.servers);
  const total = payload.servers.reduce(
    (count, server) => count + server.tools.length,
    0,
  );
  // A draft in progress is kept: the mention is appended to it and the whole
  // thing is sent, so a click never silently discards what the user typed.
  const runTool = (toolName: string) => {
    // The thread's composer, not `aui.composer()`: inside a message part that
    // resolves to the edit composer, which is only live while a message is
    // being edited and throws otherwise.
    const composer = aui.thread().composer();
    composer.setText(appendToken(composerText, `@${toolName}`));
    composer.send();
  };

  return (
    <div className="aui-tool-search-result-root @container w-full py-2">
      <div className="border border-border bg-card text-card-foreground">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1 border-b border-border px-4 py-3">
          <span
            aria-hidden
            className="size-2 shrink-0 rounded-full bg-emerald-500"
          />
          <span className="text-sm font-medium">
            {total} {total === 1 ? "tool" : "tools"} available in this session
          </span>
        </div>

        {rows.map((row) => (
          <div
            key={row.key}
            className="flex flex-col gap-2 border-b border-border px-4 py-3 last:border-b-0 @md:flex-row @md:gap-4"
          >
            <div className="flex shrink-0 flex-col gap-0.5 @md:w-40">
              <span className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
                {row.category}
              </span>
              <span className="text-xs text-muted-foreground">
                {row.tools.length} {row.tools.length === 1 ? "tool" : "tools"}
                {row.server ? ` · ${row.server}` : ""}
              </span>
            </div>
            <div className="flex min-w-0 flex-wrap gap-1.5">
              {row.tools.map((tool) => (
                <button
                  key={tool.name}
                  type="button"
                  title={payload.briefs.get(tool.name) ?? `Run ${tool.label}`}
                  onClick={() => runTool(tool.name)}
                  className={cn(
                    "flex items-center gap-1 border border-border bg-background px-2 py-1",
                    "cursor-pointer font-mono text-xs text-foreground transition-colors hover:border-foreground",
                  )}
                >
                  <PlusIcon className="size-3 text-muted-foreground" />
                  {tool.label}
                </button>
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
};
