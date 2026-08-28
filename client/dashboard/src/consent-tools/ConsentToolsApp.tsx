import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { CfWorkerJsonSchemaValidator } from "@modelcontextprotocol/sdk/validation/cfworker";
import {
  StreamableHTTPClientTransport,
  StreamableHTTPError,
} from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import { AlertTriangle, Check } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

import {
  ANNOTATION_OPTIONS,
  type ToolAnnotation,
} from "@/components/tool-selection/annotations";
import { cn } from "@/lib/utils";

import { isRecord, type ConsentSelection } from "./prefill";

export interface ConsentToolsAppProps {
  toolsUrl: string;
  state: string;
  csrfToken: string;
  formId: string;
  approveButtonId: string;
  consentEnabled: boolean;
  serverName: string;
  prefill: ConsentSelection;
}

interface ConsentTool {
  name: string;
  annotations: ToolAnnotation[];
}

interface RoleHiddenTools {
  /** Full count of tools the caller's role hides from this inventory. */
  count: number;
  /** Hidden tool names; the server caps the list, so it may trail count. */
  names: string[];
}

interface ConsentInventory {
  tools: ConsentTool[];
  roleHiddenTools: RoleHiddenTools;
}

type ConsentPhase =
  | { name: "loading" }
  | { name: "error"; conflict: boolean; message: string }
  | { name: "ready"; inventory: ConsentInventory };

/**
 * ConsentGroup identifies one left-rail entry: the whole inventory, one
 * annotation, or the tools declaring no annotation at all.
 */
type ConsentGroup = "all" | ToolAnnotation | "none";

/**
 * ToolAccessMode is the top-level choice above the picker: grant the server's
 * whole tool surface, or narrow it. "all" is the pre-picker status quo and the
 * safe default; "specific" is what reveals the picker.
 */
type ToolAccessMode = "all" | "specific";

const GENERIC_ERROR_MESSAGE = "Couldn't load this server's tools.";

/**
 * The island is an ordinary MCP client of the consent-scoped transport: the
 * official SDK performs initialize, paginated tools/list, and session
 * termination against the URL the page provided; state and CSRF ride
 * headers so every message stays pure MCP. The attempt id keys the
 * server-side inventory snapshot this session accumulates — approval later
 * binds to it, so the caller must submit it only after this resolves.
 */
async function fetchConsentInventory(
  toolsUrl: string,
  state: string,
  csrfToken: string,
  attempt: string,
): Promise<ConsentInventory> {
  const transport = new StreamableHTTPClientTransport(
    new URL(toolsUrl, window.location.origin),
    {
      requestInit: {
        credentials: "omit",
        headers: {
          "Gram-Consent-State": state,
          "Gram-Consent-Csrf": csrfToken,
          "Gram-Consent-Inventory-Attempt": attempt,
        },
      },
    },
  );
  // The consent page CSP has no unsafe-eval, so the SDK's default Ajv
  // validator (new Function codegen) throws on any upstream tool that
  // declares an outputSchema. The cfworker validator interprets schemas
  // instead of compiling them.
  const client = new Client(
    { name: "gram-consent", version: "1.0.0" },
    { jsonSchemaValidator: new CfWorkerJsonSchemaValidator() },
  );
  try {
    await client.connect(transport);
    const tools: ConsentTool[] = [];
    const seen = new Set<string>();
    const roleHiddenTools: RoleHiddenTools = { count: 0, names: [] };
    let cursor: string | undefined = undefined;
    do {
      const page = await client.listTools(cursor ? { cursor } : undefined);
      for (const tool of page.tools) {
        if (seen.has(tool.name)) throw new Error("duplicate tool name");
        seen.add(tool.name);
        tools.push({
          name: tool.name,
          annotations: annotationsFromHints(tool.annotations),
        });
      }
      const meta = (page as { _meta?: Record<string, unknown> })._meta;
      const hidden = meta?.["gram.dev/roleHiddenTools"];
      if (isRecord(hidden)) {
        if (typeof hidden.count === "number" && hidden.count > 0) {
          roleHiddenTools.count += hidden.count;
        }
        if (Array.isArray(hidden.names)) {
          for (const name of hidden.names) {
            if (typeof name === "string" && name !== "") {
              roleHiddenTools.names.push(name);
            }
          }
        }
      }
      cursor = page.nextCursor;
    } while (cursor);
    return { tools, roleHiddenTools };
  } finally {
    await transport.terminateSession().catch(() => undefined);
    await client.close().catch(() => undefined);
  }
}

const HINT_TO_ANNOTATION: ReadonlyArray<[string, ToolAnnotation]> = [
  ["readOnlyHint", "read_only"],
  ["destructiveHint", "destructive"],
  ["idempotentHint", "idempotent"],
  ["openWorldHint", "open_world"],
];

function annotationsFromHints(value: unknown): ToolAnnotation[] {
  if (!isRecord(value)) return [];
  return HINT_TO_ANNOTATION.filter(([hint]) => value[hint] === true).map(
    ([, annotation]) => annotation,
  );
}

function errorPhase(err: unknown): ConsentPhase {
  const status = err instanceof StreamableHTTPError ? err.code : undefined;
  if (status === 409) {
    return {
      name: "error",
      conflict: true,
      message: "The upstream service is not connected.",
    };
  }
  return { name: "error", conflict: false, message: GENERIC_ERROR_MESSAGE };
}

function groupLabel(group: ConsentGroup): string {
  if (group === "all") return "All tools";
  if (group === "none") return "No annotation";
  return ANNOTATION_OPTIONS.find((o) => o.key === group)?.label ?? group;
}

function groupTools(tools: ConsentTool[], group: ConsentGroup): ConsentTool[] {
  if (group === "all") return tools;
  if (group === "none") return tools.filter((t) => t.annotations.length === 0);
  return tools.filter((t) => t.annotations.includes(group));
}

export function ConsentToolsApp({
  toolsUrl,
  state,
  csrfToken,
  formId,
  approveButtonId,
  consentEnabled,
  serverName,
  prefill,
}: ConsentToolsAppProps): JSX.Element {
  const [phase, setPhase] = useState<ConsentPhase>({ name: "loading" });
  const [attemptID, setAttemptID] = useState(() => crypto.randomUUID());
  // Tool access is a two-step choice: almost nobody narrows the grant, so the
  // picker stays behind "Specific tools" and "All tools" is the one-glance
  // default. The mode is the only thing that sets the all-tools grant — inside
  // the picker every control edits annotations and picks.
  // Scope = allGrant ? everything : (annotation matches ∪ picks).
  const [mode, setMode] = useState<ToolAccessMode>(
    prefill === null ? "all" : "specific",
  );
  const allGrant = mode === "all";
  const [annGrants, setAnnGrants] = useState<
    ReadonlyMap<ToolAnnotation, "snapshot" | "live">
  >(new Map());
  const [picked, setPicked] = useState<ReadonlySet<string>>(new Set());
  const [nav, setNav] = useState<ConsentGroup>("all");
  const [droppedPrefillGrants, setDroppedPrefillGrants] = useState(0);
  const [query, setQuery] = useState("");

  useEffect(() => {
    let cancelled = false;
    setPhase({ name: "loading" });
    const load = async () => {
      const inventory = await fetchConsentInventory(
        toolsUrl,
        state,
        csrfToken,
        attemptID,
      );
      if (cancelled) return;
      if (prefill === null) {
        setMode("all");
        setAnnGrants(new Map());
        setPicked(new Set());
      } else {
        const known = new Set(inventory.tools.map((t) => t.name));
        const grants = new Map<ToolAnnotation, "snapshot" | "live">();
        let dropped = 0;
        for (const grant of prefill.annotations) {
          // A stored grant whose annotation no longer matches any displayed
          // tool would be invisible in the picker yet rejected at approval:
          // discard it (fail narrow) and say so.
          const matches = inventory.tools.some((t) =>
            t.annotations.includes(grant.name),
          );
          if (!matches) {
            dropped += 1;
            continue;
          }
          grants.set(grant.name, grant.mode === "live" ? "live" : "snapshot");
        }
        setDroppedPrefillGrants(dropped);
        setMode("specific");
        setAnnGrants(grants);
        setPicked(new Set(prefill.tools.filter((t) => known.has(t))));
      }
      setPhase({ name: "ready", inventory });
    };
    load().catch((err: unknown) => {
      if (!cancelled) setPhase(errorPhase(err));
    });
    return () => {
      cancelled = true;
    };
  }, [attemptID, toolsUrl, state, csrfToken, prefill]);

  // Approval is gated on both a successfully loaded inventory and the
  // server-rendered remote-session readiness flag; anything else (loading,
  // error, retry, malformed response) keeps the button disabled.
  const approveReady = phase.name === "ready" && consentEnabled;
  useEffect(() => {
    const button = document.getElementById(approveButtonId);
    if (button instanceof HTMLButtonElement) {
      button.disabled = !approveReady;
    }
  }, [approveButtonId, approveReady]);

  const tools = useMemo(
    (): ConsentTool[] => (phase.name === "ready" ? phase.inventory.tools : []),
    [phase],
  );

  const scope = useMemo((): ReadonlySet<string> => {
    if (allGrant) return new Set(tools.map((t) => t.name));
    const names = new Set(picked);
    for (const tool of tools) {
      if (tool.annotations.some((a) => annGrants.has(a))) names.add(tool.name);
    }
    return names;
  }, [allGrant, annGrants, picked, tools]);

  // The form submits grant INTENT: annotation names split by mode plus the
  // individually picked tools. The server derives snapshot expansions from
  // its display-bound inventory snapshot — expansions never ride the form.
  const snapshotAnnotations = useMemo(
    (): ToolAnnotation[] =>
      [...annGrants].filter(([, m]) => m === "snapshot").map(([a]) => a),
    [annGrants],
  );
  const liveAnnotations = useMemo(
    (): ToolAnnotation[] =>
      [...annGrants].filter(([, m]) => m === "live").map(([a]) => a),
    [annGrants],
  );

  if (phase.name === "loading") {
    return (
      <div
        role="status"
        aria-live="polite"
        className="border-border text-muted-foreground border px-3 py-3 text-sm"
      >
        Loading available tools…
      </div>
    );
  }

  if (phase.name === "error") {
    return (
      <div
        role="alert"
        className="border-border space-y-1 border px-3 py-3 text-sm"
      >
        <p className="text-muted-foreground flex items-center gap-1.5">
          <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
          {phase.message}
        </p>
        {phase.conflict && (
          <p className="text-muted-foreground">
            Connect the service above, then retry.
          </p>
        )}
        <button
          type="button"
          onClick={() => setAttemptID(crypto.randomUUID())}
          className="text-primary hover:underline"
        >
          Retry
        </button>
      </div>
    );
  }

  const groups: { id: ConsentGroup; label: string; count: number }[] = [
    { id: "all", label: "All tools", count: tools.length },
    ...ANNOTATION_OPTIONS.map((o) => ({
      id: o.key as ConsentGroup,
      label: o.label,
      count: groupTools(tools, o.key).length,
    })).filter((g) => g.count > 0),
    ...(groupTools(tools, "none").length > 0
      ? [
          {
            id: "none" as ConsentGroup,
            label: "No annotation",
            count: groupTools(tools, "none").length,
          },
        ]
      : []),
  ];
  const grouped = groupTools(tools, nav);
  const normalizedQuery = query.trim().toLowerCase();
  const viewed =
    normalizedQuery === ""
      ? grouped
      : grouped.filter((t) => t.name.toLowerCase().includes(normalizedQuery));

  const groupGranted = (id: ConsentGroup): boolean => {
    if (id === "all") {
      return tools.length > 0 && tools.every((t) => scope.has(t.name));
    }
    if (id === "none") {
      const unlabeled = groupTools(tools, "none");
      return unlabeled.length > 0 && unlabeled.every((t) => scope.has(t.name));
    }
    return annGrants.has(id);
  };

  // Bulk-select the tools in the current nav group by name. Used for the
  // groups that have no annotation grant to express them: "All tools" and the
  // unannotated bucket.
  const toggleGroupPicks = (group: ConsentGroup) => {
    const members = groupTools(tools, group);
    const every = members.every((t) => picked.has(t.name));
    const next = new Set(picked);
    members.forEach((t) => {
      if (every) next.delete(t.name);
      else next.add(t.name);
    });
    setPicked(next);
  };

  const toggleGroupGrant = () => {
    if (nav === "all" || nav === "none") {
      toggleGroupPicks(nav);
      return;
    }
    const next = new Map(annGrants);
    if (next.has(nav)) next.delete(nav);
    // New grants default to live: like the all-tools grant, matching tools
    // the server adds later are included until the user freezes the grant.
    else next.set(nav, "live");
    setAnnGrants(next);
  };

  const togglePick = (name: string) => {
    const next = new Set(picked);
    if (next.has(name)) next.delete(name);
    else next.add(name);
    setPicked(next);
  };

  const pickedOutsideGrants = allGrant
    ? 0
    : [...picked].filter(
        (n) =>
          !tools.some(
            (t) => t.name === n && t.annotations.some((a) => annGrants.has(a)),
          ),
      ).length;

  return (
    <div className="flex flex-col gap-3">
      <ToolAccessModeChoice
        mode={mode}
        onChange={setMode}
        toolCount={tools.length}
      />
      {mode === "specific" && (
        <div className="border-border grid grid-cols-[9.5rem_1fr] border">
          <nav
            aria-label="Tool groups"
            className="border-border flex flex-col border-r py-1"
          >
            {groups.map((g) => (
              <button
                key={g.id}
                type="button"
                aria-current={nav === g.id}
                onClick={() => setNav(g.id)}
                className={cn(
                  "hover:bg-accent flex cursor-pointer items-center gap-1.5 px-2.5 py-1.5 text-left text-sm",
                  nav === g.id && "bg-accent font-medium",
                )}
              >
                <span className="flex w-3.5 shrink-0 justify-center">
                  {groupGranted(g.id) && (
                    <Check
                      aria-label="granted"
                      className="text-success h-3 w-3"
                    />
                  )}
                </span>
                <span className="min-w-0 flex-1 truncate">{g.label}</span>
                <span className="text-muted-foreground text-xs">{g.count}</span>
              </button>
            ))}
          </nav>
          <div className="flex min-w-0 flex-col">
            <div className="border-border flex items-center justify-between border-b px-3 py-2">
              <span className="text-sm font-medium">{groupLabel(nav)}</span>
              <div className="flex items-center gap-3">
                {nav !== "all" && nav !== "none" && annGrants.has(nav) && (
                  <button
                    type="button"
                    role="checkbox"
                    aria-checked={annGrants.get(nav) === "live"}
                    onClick={() => {
                      const next = new Map(annGrants);
                      next.set(
                        nav,
                        next.get(nav) === "live" ? "snapshot" : "live",
                      );
                      setAnnGrants(next);
                    }}
                    className="text-muted-foreground flex cursor-pointer items-center gap-1.5 text-xs"
                  >
                    <SelectionBox checked={annGrants.get(nav) === "live"} />
                    Include future matching tools
                  </button>
                )}
                <button
                  type="button"
                  role="checkbox"
                  aria-checked={groupGranted(nav)}
                  onClick={toggleGroupGrant}
                  className="flex cursor-pointer items-center gap-1.5 text-sm"
                >
                  <SelectionBox checked={groupGranted(nav)} />
                  All {grouped.length}
                </button>
              </div>
            </div>
            <input
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={`Search ${groupLabel(nav).toLowerCase()}…`}
              aria-label="Search tools"
              className="border-border placeholder:text-muted-foreground border-b px-3 py-1.5 text-sm outline-none"
            />
            <div className="max-h-[300px] min-h-0 overflow-y-auto">
              {viewed.map((tool) => {
                const viaGrant =
                  allGrant || tool.annotations.some((a) => annGrants.has(a));
                const inScope = scope.has(tool.name);
                return (
                  <div
                    key={tool.name}
                    role="checkbox"
                    aria-checked={inScope}
                    aria-disabled={viaGrant}
                    tabIndex={viaGrant ? -1 : 0}
                    onClick={viaGrant ? undefined : () => togglePick(tool.name)}
                    onKeyDown={
                      viaGrant
                        ? undefined
                        : (e) => {
                            if (e.key === " " || e.key === "Enter") {
                              e.preventDefault();
                              togglePick(tool.name);
                            }
                          }
                    }
                    className={cn(
                      "flex items-center gap-2 px-3 py-1",
                      viaGrant
                        ? "cursor-default"
                        : "hover:bg-accent cursor-pointer",
                    )}
                  >
                    <SelectionBox checked={inScope} />
                    <span
                      className="min-w-0 flex-1 truncate font-mono text-xs"
                      title={tool.name}
                    >
                      {tool.name}
                    </span>
                    {!allGrant && viaGrant && (
                      <span className="text-muted-foreground shrink-0 text-[10px]">
                        via annotation
                      </span>
                    )}
                    {!viaGrant && picked.has(tool.name) && (
                      <span className="text-muted-foreground shrink-0 text-[10px]">
                        picked
                      </span>
                    )}
                  </div>
                );
              })}
              {viewed.length === 0 && (
                <p className="text-muted-foreground px-3 py-2 text-sm">
                  {normalizedQuery === ""
                    ? "No tools in this group."
                    : "No tools match your search."}
                </p>
              )}
            </div>
          </div>
        </div>
      )}
      {phase.inventory.roleHiddenTools.count > 0 && (
        <RoleHiddenNote hidden={phase.inventory.roleHiddenTools} />
      )}
      {droppedPrefillGrants > 0 && (
        <p className="text-muted-foreground pt-1 text-xs">
          {droppedPrefillGrants === 1
            ? "One previously granted annotation no longer matches any tool and was removed."
            : `${droppedPrefillGrants} previously granted annotations no longer match any tool and were removed.`}
        </p>
      )}
      {mode === "specific" && (
        <div className="text-muted-foreground flex flex-wrap items-center justify-between gap-x-3 gap-y-0.5 text-xs">
          <span>
            <b className="text-foreground font-medium">{scope.size}</b> of{" "}
            {tools.length} tools in scope on {serverName}
            {annGrants.size > 0 && (
              <>
                {" · "}
                {[...annGrants]
                  .map(
                    ([annotation, grantMode]) =>
                      groupLabel(annotation).toLowerCase() +
                      (grantMode === "live" ? " (live)" : " (frozen)"),
                  )
                  .join(", ")}
              </>
            )}
            {pickedOutsideGrants > 0 && ` · ${pickedOutsideGrants} picked`}
          </span>
          <span>
            {liveAnnotations.length > 0
              ? "Live grants include future matching tools"
              : "New tools require approval"}
          </span>
        </div>
      )}
      <ApprovalFormFields
        formId={formId}
        inventoryID={attemptID}
        allGrant={allGrant}
        snapshotAnnotations={snapshotAnnotations}
        liveAnnotations={liveAnnotations}
        tools={allGrant ? [] : [...picked].sort()}
      />
    </div>
  );
}

/**
 * The top-level tool-access choice. Selecting a subset is the rare case, so the
 * picker stays collapsed behind "Specific tools" and the default reads as one
 * line rather than a wall of checkboxes.
 */
function ToolAccessModeChoice({
  mode,
  onChange,
  toolCount,
}: {
  mode: ToolAccessMode;
  onChange: (mode: ToolAccessMode) => void;
  toolCount: number;
}): JSX.Element {
  const options: {
    value: ToolAccessMode;
    label: string;
    description: string;
  }[] = [
    {
      value: "all",
      label: "All tools",
      description:
        toolCount === 1
          ? "The server's single tool, plus any it adds later."
          : `All ${toolCount} tools, plus any the server adds later.`,
    },
    {
      value: "specific",
      label: "Specific tools",
      description: "Choose which tools this client may call.",
    },
  ];

  return (
    <div
      role="radiogroup"
      aria-label="Tool access"
      className="border-border border"
    >
      {options.map((option, index) => (
        <label
          key={option.value}
          className={cn(
            "hover:bg-accent flex cursor-pointer items-start gap-2.5 px-3 py-2.5",
            index > 0 && "border-border border-t",
            mode === option.value && "bg-accent",
          )}
        >
          <input
            type="radio"
            name="consent-tool-access-mode"
            value={option.value}
            checked={mode === option.value}
            onChange={() => onChange(option.value)}
            className="sr-only"
          />
          <span
            aria-hidden
            className={cn(
              "border-border mt-0.5 flex size-3.5 shrink-0 items-center justify-center rounded-full border",
              mode === option.value && "border-foreground",
            )}
          >
            {mode === option.value && (
              <span className="bg-foreground size-1.5 rounded-full" />
            )}
          </span>
          <span className="min-w-0">
            <span className="block text-sm font-medium">{option.label}</span>
            <span className="text-muted-foreground block text-xs">
              {option.description}
            </span>
          </span>
        </label>
      ))}
    </div>
  );
}

/**
 * The RBAC-narrowing disclosure: hover or focus reveals which tool names the
 * caller's role excluded. Hand-rolled popover rather than the Radix tooltip
 * because the island portals nothing — Radix mounts content on document.body,
 * outside the island's scoped preflight and theme tokens.
 */
function RoleHiddenNote({ hidden }: { hidden: RoleHiddenTools }): JSX.Element {
  const label =
    hidden.count === 1
      ? "1 tool is hidden by your role and cannot be granted here."
      : `${hidden.count} tools are hidden by your role and cannot be granted here.`;
  const undisclosed = hidden.count - hidden.names.length;
  return (
    <div className="group relative w-fit pt-1">
      <p
        tabIndex={0}
        className="text-muted-foreground cursor-help text-xs underline decoration-dotted underline-offset-2"
      >
        {label}
      </p>
      {hidden.names.length > 0 && (
        <div
          role="tooltip"
          className="border-border bg-background absolute bottom-full left-0 z-10 mb-1 hidden max-h-44 w-max max-w-72 overflow-y-auto rounded-md border px-3 py-2 shadow-md group-focus-within:block group-hover:block"
        >
          <ul className="m-0 list-none space-y-0.5 p-0 font-mono text-xs">
            {hidden.names.map((name) => (
              <li key={name}>{name}</li>
            ))}
          </ul>
          {undisclosed > 0 && (
            <p className="text-muted-foreground pt-1 text-xs">
              and {undisclosed} more
            </p>
          )}
        </div>
      )}
    </div>
  );
}

function SelectionBox({ checked }: { checked: boolean }): JSX.Element {
  return (
    <span
      aria-hidden
      className={cn(
        "border-border flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded-[3px] border",
        checked && "bg-foreground border-foreground",
      )}
    >
      {checked && <Check className="text-background h-2.5 w-2.5" />}
    </span>
  );
}

/**
 * Contributes the exact tool-filtering fields to the server-rendered approve
 * form: annotation grant names split by mode (tool_annotations /
 * tool_annotations_live) and individually picked tool names (tools). The
 * server derives snapshot expansions from its own display-bound snapshot.
 * The "all tools" grant maps to tool_filtering=off. tool_inventory_id names
 * the attempt whose completed snapshot approval binds to; it renders only
 * in the ready state (this component is only mounted then), so a submit can
 * never carry a half-fetched attempt.
 */
function ApprovalFormFields({
  formId,
  inventoryID,
  allGrant,
  snapshotAnnotations,
  liveAnnotations,
  tools,
}: {
  formId: string;
  inventoryID: string;
  allGrant: boolean;
  snapshotAnnotations: ToolAnnotation[];
  liveAnnotations: ToolAnnotation[];
  tools: string[];
}): JSX.Element {
  const inventoryField = (
    <input
      type="hidden"
      name="tool_inventory_id"
      value={inventoryID}
      form={formId}
    />
  );
  if (allGrant) {
    return (
      <>
        {inventoryField}
        <input type="hidden" name="tool_filtering" value="off" form={formId} />
      </>
    );
  }
  return (
    <>
      {inventoryField}
      <input type="hidden" name="tool_filtering" value="on" form={formId} />
      {snapshotAnnotations.map((annotation) => (
        <input
          key={annotation}
          type="hidden"
          name="tool_annotations"
          value={annotation}
          form={formId}
        />
      ))}
      {liveAnnotations.map((annotation) => (
        <input
          key={annotation}
          type="hidden"
          name="tool_annotations_live"
          value={annotation}
          form={formId}
        />
      ))}
      {tools.map((tool) => (
        <input
          key={tool}
          type="hidden"
          name="tools"
          value={tool}
          form={formId}
        />
      ))}
    </>
  );
}
