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
  extractPayload,
  type ServerStatus,
} from "@/elements/components/assistant-ui/tool-search-result.helpers";
import { toolSearchVerdict } from "@/elements/components/assistant-ui/tool-widget-rendering";
import { useElements } from "@/elements/hooks/useElements";
import { appendToken } from "@/elements/lib/tool-mentions";

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
 * Only a browse search draws the card, and only where the card can render
 * outside its run's collapsible; anything else renders the generic tool row
 * instead. See `toolSearchVerdict`.
 *
 * It goes through the model rather than calling the tool directly because the
 * tools live in the assistant's runtime, not in the page — and most of them
 * take arguments the card has no way to fill. The model reads the mention,
 * works out the arguments, and calls it.
 */
export const ToolSearchResult: ToolCallMessagePartComponent = (props) => {
  const { result, toolCallId } = props;
  const aui = useAui();
  const { config } = useElements();
  const composerText = useAuiState(({ thread }) => thread.composer.text);

  // The state's own array, not one built in the selector: useAuiState compares
  // snapshots by identity, and a fresh array every render would never settle.
  const parts = useAuiState(({ message }) => message.parts);

  // Which of the message's searches draws is a question about the whole
  // message — see `toolSearchVerdict`.
  const hostComponents = config.tools?.components;
  const verdict = useMemo(
    () => toolSearchVerdict(parts, toolCallId, hostComponents),
    [parts, toolCallId, hostComponents],
  );

  // No status check of its own: `extractPayload` is null for a call that has
  // not come back with a readable catalog, which is the same test the verdict
  // used to pick this call.
  const payload = useMemo(
    () => (verdict === "draw" ? extractPayload(result) : null),
    [result, verdict],
  );

  if (!payload) {
    // A duplicate browse renders nothing; everything else — a discovery
    // search, a browse stranded in a collapsed run, a call still streaming, a
    // denied call, an error, a search that reached no connected server —
    // belongs to the generic tool card, which already renders those states. A
    // host that replaced that card keeps it here: declining is the common path
    // now that discovery searches take it.
    if (verdict === "suppress") return null;
    const Fallback = config.components?.ToolFallback ?? ToolFallback;
    return <Fallback {...props} />;
  }

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
