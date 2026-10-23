import { StreamableHTTPError } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import { AlertTriangle, Check, Minus } from "lucide-react";
import { useEffect, useLayoutEffect, useMemo, useState } from "react";

import {
  ANNOTATION_OPTIONS,
  type ToolAnnotation,
} from "@/components/tool-selection/annotations";
import { cn } from "@/lib/utils";

import type { ConsentSelection } from "./prefill";
import {
  fetchConsentInventory,
  type ConsentInventory,
  type ConsentTool,
  type RoleHiddenTools,
} from "./consentInventory";

export interface ConsentToolsAppProps {
  toolsUrl: string;
  state: string;
  csrfToken: string;
  formId: string;
  approveButtonId: string;
  consentEnabled: boolean;
  /** Gateway choices bind to a complete member inventory. */
  gateway?: boolean;
  gatewayInventory?: ConsentInventory & { attempt: string };
  /**
   * Set by the server when every service is linked or reached through
   * identity chaining: a "not connected" inventory then only means chaining
   * runs after approval, so access is given with all tools.
   */
  inventoryDeferred: boolean;
  serverName: string;
  prefill: ConsentSelection;
}

/** A gateway qualifies tool names as `<server slug>--<tool>`. */
const QUALIFIED_NAME_SEPARATOR = "--";

/** The name to show for a tool: a gateway tool drops its server prefix. */
function displayName(tool: ConsentTool): string {
  if (!tool.source) return tool.name;
  const at = tool.name.indexOf(QUALIFIED_NAME_SEPARATOR);
  return at > 0
    ? tool.name.slice(at + QUALIFIED_NAME_SEPARATOR.length)
    : tool.name;
}

/**
 * The member server a gateway tool belongs to: its slug prefix, which is
 * unique per gateway where the display name is not.
 */
function sourceKey(tool: ConsentTool): string {
  if (!tool.source) return "";
  const at = tool.name.indexOf(QUALIFIED_NAME_SEPARATOR);
  return at > 0 ? tool.name.slice(0, at) : tool.source;
}

/**
 * Section headers per source key: the member's display name, plus its slug
 * when another member shares that name.
 */
function sourceLabels(tools: ConsentTool[]): Map<string, string> {
  const names = new Map<string, string>();
  for (const tool of tools) {
    if (tool.source) names.set(sourceKey(tool), tool.source);
  }
  const uses = new Map<string, number>();
  for (const name of names.values()) uses.set(name, (uses.get(name) ?? 0) + 1);
  const labels = new Map<string, string>();
  for (const [key, name] of names) {
    labels.set(key, (uses.get(name) ?? 0) > 1 ? `${name} (${key})` : name);
  }
  return labels;
}

/** Groups tools by source key, keeping each source in first-seen order. */
function toolsBySource(tools: ConsentTool[]): [string, ConsentTool[]][] {
  const sections = new Map<string, ConsentTool[]>();
  for (const tool of tools) {
    const key = sourceKey(tool);
    const section = sections.get(key);
    if (section) section.push(tool);
    else sections.set(key, [tool]);
  }
  for (const section of sections.values()) {
    section.sort((a, b) => displayName(a).localeCompare(displayName(b)));
  }
  return [...sections];
}

type ConsentPhase =
  | { name: "loading" }
  | {
      name: "error";
      conflict: boolean;
      message: string;
      reload?: boolean;
      permanent?: boolean;
    }
  | { name: "deferred" }
  | { name: "unrestricted"; message: string }
  | { name: "ready"; inventory: ConsentInventory };

/**
 * ConsentGroup identifies one left-rail entry: the whole inventory, one
 * annotation, or the tools declaring no annotation at all.
 */
type ConsentGroup = "all" | ToolAnnotation | "none";

/**
 * ToolAccessMode chooses live tools, a cached catalog, or a selected subset.
 * Specific tools reveals the picker and uses cached gateway discovery.
 */
type ToolAccessMode = "all" | "cached" | "specific";

const GENERIC_ERROR_MESSAGE = "Couldn't load this server's tools.";

const UNSUPPORTED_MESSAGE =
  "This server has too many tools to choose from here.";

/**
 * The consent page script raises this, and sets the matching attribute on
 * the document element, when any identity-chained service is refused. A
 * deferred page is chain-only, so its inventory can no longer load after
 * approval.
 */
export const CHAIN_REJECTED_EVENT = "gram:consent-chain-rejected";
const CHAIN_REJECTED_ATTRIBUTE = "data-consent-chain-rejected";

const CHAIN_REJECTED_PHASE: ConsentPhase = {
  name: "error",
  conflict: true,
  message:
    "Your identity provider couldn't connect this service, so its tools can't load.",
};

function chainRejectedOnPage(): boolean {
  return (
    document.documentElement.getAttribute(CHAIN_REJECTED_ATTRIBUTE) === "true"
  );
}

/**
 * Chip labels for the group filters. Deliberately terse: all six groups have to
 * sit on one line at the consent card's width, and the full label still names
 * the group in the "Select all in …" control below them.
 */
const SHORT_GROUP_LABELS: Record<ToolAnnotation, string> = {
  read_only: "Read",
  destructive: "Destructive",
  idempotent: "Idempotent",
  open_world: "Open world",
};

function errorPhase(
  err: unknown,
  canDefer: boolean,
  hasPrefill: boolean,
): ConsentPhase {
  const status = err instanceof StreamableHTTPError ? err.code : undefined;
  if (status === 412) {
    return {
      name: "error",
      conflict: false,
      message:
        "This server’s configuration changed. Reload this page to review access.",
      reload: true,
    };
  }
  // The server could not capture the inventory for selection (too many
  // tools or unsupported definitions). A required capture cannot be approved.
  if (status === 413 || status === 422) {
    return hasPrefill
      ? {
          name: "error",
          conflict: false,
          permanent: true,
          message: UNSUPPORTED_MESSAGE,
        }
      : { name: "unrestricted", message: UNSUPPORTED_MESSAGE };
  }
  if (status === 409 && canDefer) {
    return { name: "deferred" };
  }
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

function initialAccessMode(
  prefill: ConsentSelection,
  gateway: boolean,
): ToolAccessMode {
  if (prefill?.accessMode) return prefill.accessMode;
  if (prefill !== null) return "specific";
  return gateway ? "cached" : "all";
}

export function ConsentToolsApp({
  toolsUrl,
  state,
  csrfToken,
  formId,
  approveButtonId,
  consentEnabled,
  gateway = false,
  gatewayInventory,
  inventoryDeferred,
  serverName,
  prefill,
}: ConsentToolsAppProps): JSX.Element {
  const [loadPhase, setPhase] = useState<ConsentPhase>({ name: "loading" });
  const [attemptID, setAttemptID] = useState(
    () => gatewayInventory?.attempt ?? crypto.randomUUID(),
  );
  const [mode, setMode] = useState<ToolAccessMode>(
    initialAccessMode(prefill, gateway),
  );
  const allGrant = mode !== "specific";
  const catalogMode = mode === "all" ? "live" : "cached";
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
      let inventory: ConsentInventory;
      if (gateway) {
        if (!gatewayInventory)
          throw new Error("Gateway inventory is missing; reload consent.");
        inventory = gatewayInventory;
      } else {
        inventory = await fetchConsentInventory(
          toolsUrl,
          state,
          csrfToken,
          attemptID,
        );
      }
      if (cancelled) return;
      if (prefill === null) {
        setMode(gateway ? "cached" : "all");
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
          grants.set(
            grant.name,
            !gateway && grant.mode === "live" ? "live" : "snapshot",
          );
        }
        setDroppedPrefillGrants(dropped);
        setMode(prefill.accessMode ?? "specific");
        setAnnGrants(grants);
        setPicked(new Set(prefill.tools.filter((t) => known.has(t))));
      }
      setPhase({ name: "ready", inventory });
    };
    load().catch((err: unknown) => {
      // A stored narrower selection must never be replaced by all tools.
      if (!cancelled) {
        setPhase(
          errorPhase(
            err,
            !gateway && inventoryDeferred && prefill === null,
            prefill !== null,
          ),
        );
      }
    });
    return () => {
      cancelled = true;
    };
  }, [
    attemptID,
    toolsUrl,
    state,
    csrfToken,
    prefill,
    inventoryDeferred,
    gateway,
    gatewayInventory,
  ]);

  const [chainRejected, setChainRejected] = useState(chainRejectedOnPage);
  useEffect(() => {
    const onRejected = () => setChainRejected(true);
    document.addEventListener(CHAIN_REJECTED_EVENT, onRejected);
    if (chainRejectedOnPage()) setChainRejected(true);
    return () => document.removeEventListener(CHAIN_REJECTED_EVENT, onRejected);
  }, []);
  const phase: ConsentPhase =
    loadPhase.name === "deferred" && chainRejected
      ? CHAIN_REJECTED_PHASE
      : loadPhase;

  // Approval is gated on both a successfully loaded inventory and the
  // server-rendered remote-session readiness flag; anything else (loading,
  // error, retry, malformed response) keeps the button disabled. A layout
  // effect keeps the server-rendered button in step with the commit, so it is
  // never enabled while the island already shows a blocking error.
  const approveReady =
    (phase.name === "ready" ||
      (!gateway &&
        (phase.name === "deferred" || phase.name === "unrestricted"))) &&
    consentEnabled;
  useLayoutEffect(() => {
    const button = document.getElementById(approveButtonId);
    if (button instanceof HTMLButtonElement) {
      button.dataset["consentSelfReady"] = approveReady ? "true" : "false";
      if (button.dataset["agentSelected"] !== "true") {
        button.disabled = !approveReady;
      }
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

  if (phase.name === "deferred") {
    return (
      <div
        role="status"
        className="border-border text-muted-foreground space-y-1 border px-3 py-3 text-sm"
      >
        <p>
          Tools load after you give access. This connection starts with all
          tools available to you, because tool selection needs the service
          connected first.
        </p>
        <input type="hidden" name="tool_filtering" value="off" form={formId} />
        <p>
          Signed in to the service separately?{" "}
          <button
            type="button"
            onClick={() => setAttemptID(crypto.randomUUID())}
            className="text-primary hover:underline"
          >
            Load tools
          </button>
        </p>
      </div>
    );
  }

  if (phase.name === "unrestricted") {
    return (
      <div
        role="status"
        className="border-border text-muted-foreground space-y-1 border px-3 py-3 text-sm"
      >
        <p>
          {phase.message} This connection starts with all tools available to
          you.
        </p>
        <input type="hidden" name="tool_filtering" value="off" form={formId} />
        <button
          type="button"
          onClick={() => setAttemptID(crypto.randomUUID())}
          className="text-primary hover:underline"
        >
          Load tools again
        </button>
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
        <div className="flex flex-wrap gap-x-3">
          {!phase.permanent && (
            <button
              type="button"
              onClick={() => {
                if (phase.reload || gateway) window.location.reload();
                else setAttemptID(crypto.randomUUID());
              }}
              className="text-primary hover:underline"
            >
              {phase.reload || gateway ? "Reload page" : "Retry"}
            </button>
          )}
          {/* Tool selection is optional: an inventory that will not load
              must not stop the user from giving access with all tools. A
              changed configuration needs a reload before any approval. */}
          {!gateway && !phase.conflict && !phase.reload && (
            <button
              type="button"
              onClick={() =>
                setPhase({ name: "unrestricted", message: phase.message })
              }
              className="text-primary hover:underline"
            >
              Give access to all tools
            </button>
          )}
        </div>
      </div>
    );
  }

  const groups: {
    id: ConsentGroup;
    label: string;
    shortLabel: string;
    count: number;
  }[] = [
    { id: "all", label: "All tools", shortLabel: "All", count: tools.length },
    ...ANNOTATION_OPTIONS.map((o) => ({
      id: o.key as ConsentGroup,
      label: o.label,
      shortLabel: SHORT_GROUP_LABELS[o.key],
      count: groupTools(tools, o.key).length,
    })).filter((g) => g.count > 0),
    ...(groupTools(tools, "none").length > 0
      ? [
          {
            id: "none" as ConsentGroup,
            label: "No annotation",
            shortLabel: "Unannotated",
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
      : grouped.filter(
          (t) =>
            t.name.toLowerCase().includes(normalizedQuery) ||
            (t.source ?? "").toLowerCase().includes(normalizedQuery),
        );
  const hasSources = tools.some((t) => t.source !== undefined);
  const labels = sourceLabels(tools);

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
  const navGranted =
    groupGranted(nav) ||
    (grouped.length > 0 && grouped.every((t) => scope.has(t.name)));
  const navPartial = !navGranted && grouped.some((t) => scope.has(t.name));

  // Bulk-select tools by name: the "All tools" and unannotated groups, which
  // have no annotation grant to express them, and a gateway's source sections.
  const togglePicks = (members: ConsentTool[]) => {
    // Read "already granted" from the scope, the same union the checkbox
    // renders from. Reading it from picks alone leaves a group that is fully
    // covered by an annotation grant showing as checked but refusing to
    // clear, because none of its tools is individually picked.
    const granted =
      members.length > 0 && members.every((t) => scope.has(t.name));
    const next = new Set(picked);
    members.forEach((t) => {
      if (granted) next.delete(t.name);
      else next.add(t.name);
    });
    if (!granted) {
      setPicked(next);
      return;
    }
    // Dropping picks cannot revoke coverage that came from an annotation, so
    // clear every grant that reaches into this group. A grant spans the whole
    // inventory, so the tools it covered outside the group become picks and
    // stay in scope.
    const memberNames = new Set(members.map((t) => t.name));
    const remaining = new Map(annGrants);
    for (const [annotation] of annGrants) {
      if (members.some((t) => t.annotations.includes(annotation))) {
        remaining.delete(annotation);
      }
    }
    for (const tool of tools) {
      if (
        !memberNames.has(tool.name) &&
        scope.has(tool.name) &&
        !tool.annotations.some((a) => remaining.has(a))
      ) {
        next.add(tool.name);
      }
    }
    setPicked(next);
    setAnnGrants(remaining);
  };

  const toggleGroupGrant = () => {
    if (nav !== "all" && nav !== "none" && annGrants.has(nav)) {
      const remaining = new Map(annGrants);
      remaining.delete(nav);
      setAnnGrants(remaining);
      const next = new Set(picked);
      grouped.forEach((t) => {
        next.delete(t.name);
      });
      setPicked(next);
      return;
    }
    // A fully covered group clears, whether picks or a grant cover it, so
    // the checked header never adds a grant the user did not ask for.
    if (nav === "all" || nav === "none" || navGranted) {
      togglePicks(grouped);
      return;
    }
    // Gateway grants capture the matching tools shown at consent.
    setAnnGrants(new Map(annGrants).set(nav, gateway ? "snapshot" : "live"));
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

  const renderTool = (tool: ConsentTool) => {
    const viaGrant = allGrant || tool.annotations.some((a) => annGrants.has(a));
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
          "flex items-center gap-2.5 px-3 py-2",
          viaGrant ? "cursor-default" : "hover:bg-accent cursor-pointer",
        )}
      >
        <SelectionBox checked={inScope} />
        <span
          className="min-w-0 flex-1 truncate font-mono text-sm"
          title={tool.name}
        >
          {displayName(tool)}
        </span>
        {!allGrant && viaGrant && (
          <span className="text-eyebrow shrink-0">via annotation</span>
        )}
      </div>
    );
  };

  return (
    <div className="flex flex-col gap-3">
      <ToolAccessModeChoice
        mode={mode}
        gateway={gateway}
        onChange={setMode}
        toolCount={tools.length}
      />
      {mode === "specific" && (
        <div className="flex flex-col gap-2">
          <input
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search tools…"
            aria-label="Search tools"
            className="border-border placeholder:text-muted-foreground h-9 border px-3 text-sm outline-none focus:border-foreground"
          />

          {/* Annotation groups read as filters, not a second column: a rail
              stole width from the tool names it was meant to help scan. */}
          {groups.length > 1 && (
            <div
              role="group"
              aria-label="Tool groups"
              className="flex flex-wrap gap-1"
            >
              {groups.map((g) => (
                <button
                  key={g.id}
                  type="button"
                  // Filter buttons rather than a tablist: these narrow the list
                  // below rather than swapping panels, and a tablist without a
                  // linked tabpanel and arrow-key navigation is a half-built
                  // pattern for assistive technology.
                  aria-pressed={nav === g.id}
                  onClick={() => setNav(g.id)}
                  className={cn(
                    // No text-* class here: cn() runs tailwind-merge, which
                    // treats text-eyebrow as a text utility and would drop it
                    // for any text colour added alongside — taking the mono
                    // uppercase styling with it. Selection is carried by the
                    // border and background instead.
                    "text-eyebrow border-border interact:border-foreground flex cursor-pointer items-center gap-1.5 border px-2 py-1",
                    nav === g.id && "border-foreground bg-accent",
                  )}
                >
                  {groupGranted(g.id) && (
                    <Check aria-label="granted" className="h-2.5 w-2.5" />
                  )}
                  {g.shortLabel}
                  <span className="opacity-60">{g.count}</span>
                </button>
              ))}
            </div>
          )}

          <div className="border-border flex flex-col border">
            <div className="border-border flex items-center justify-between gap-3 border-b px-3 py-2">
              <button
                type="button"
                role="checkbox"
                aria-checked={bulkChecked(navGranted, navPartial)}
                onClick={toggleGroupGrant}
                className="flex cursor-pointer items-center gap-2 text-sm"
              >
                <SelectionBox checked={navGranted} indeterminate={navPartial} />
                Select all {grouped.length}
                {nav !== "all" && (
                  <span className="text-muted-foreground">
                    in {groupLabel(nav).toLowerCase()}
                  </span>
                )}
              </button>
              {!gateway &&
                nav !== "all" &&
                nav !== "none" &&
                annGrants.has(nav) && (
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
            </div>
            {/* A minimum height so a three-tool server does not render a
                letterbox, and a cap so a 200-tool one stays scannable. */}
            <div className="max-h-[19rem] min-h-[11rem] overflow-y-auto">
              {hasSources
                ? toolsBySource(viewed).map(([key, sectionTools]) => {
                    const source = labels.get(key) ?? key;
                    const sectionInScope = sectionTools.filter((t) =>
                      scope.has(t.name),
                    ).length;
                    const sectionGranted =
                      sectionInScope === sectionTools.length;
                    const sectionPartial = sectionInScope > 0;
                    return (
                      <div key={key} role="group" aria-label={source}>
                        <button
                          type="button"
                          role="checkbox"
                          aria-checked={bulkChecked(
                            sectionGranted,
                            sectionPartial,
                          )}
                          onClick={() => togglePicks(sectionTools)}
                          className="border-border bg-background hover:bg-accent sticky top-0 flex w-full cursor-pointer items-center gap-2.5 border-b px-3 py-2 text-left"
                        >
                          <SelectionBox
                            checked={sectionGranted}
                            indeterminate={sectionPartial}
                          />
                          <span
                            className="min-w-0 flex-1 truncate text-sm font-medium"
                            title={source}
                          >
                            {source}
                          </span>
                          <span className="text-muted-foreground shrink-0 text-xs">
                            {sectionInScope} of {sectionTools.length}
                          </span>
                        </button>
                        {sectionTools.map(renderTool)}
                      </div>
                    );
                  })
                : viewed.map(renderTool)}
              {viewed.length === 0 && (
                <p className="text-muted-foreground px-3 py-3 text-sm">
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
            {annGrants.size > 0 &&
              pickedOutsideGrants > 0 &&
              ` · ${pickedOutsideGrants} picked`}
          </span>
          <span>
            {liveAnnotations.length > 0
              ? hasSources
                ? "Live grants include future matching tools from the sources shown"
                : "Live grants include future matching tools"
              : "New tools require approval"}
          </span>
        </div>
      )}
      {mode === "specific" && scope.size === 0 && (
        <p
          role="status"
          className="text-muted-foreground flex items-center gap-1.5 text-xs"
        >
          <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
          No tools selected — this client won't be able to call any tools.
        </p>
      )}
      <ApprovalFormFields
        formId={formId}
        inventoryID={attemptID}
        allGrant={allGrant}
        catalogMode={gateway ? catalogMode : undefined}
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
  gateway,
  onChange,
  toolCount,
}: {
  mode: ToolAccessMode;
  gateway: boolean;
  onChange: (mode: ToolAccessMode) => void;
  toolCount: number;
}): JSX.Element {
  let liveDescription = `All ${toolCount} tools, plus any the server adds later.`;
  if (toolCount === 1) {
    liveDescription = "The server's single tool, plus any it adds later.";
  }
  if (gateway) {
    liveDescription = `${toolCount === 1 ? "The single tool" : `All ${toolCount} tools`}, plus future tools from these connected servers. New servers need your consent.`;
  }
  const options: {
    value: ToolAccessMode;
    label: string;
    description: string;
  }[] = [
    {
      value: "all",
      label: gateway ? "All tools (live)" : "All tools",
      description: liveDescription,
    },
    {
      value: "specific",
      label: "Specific tools",
      description: "Choose which tools this client may call.",
    },
  ];
  if (gateway) {
    options.splice(1, 0, {
      value: "cached",
      label: "All tools (frozen)",
      description: `Tools are fixed at consent (${toolCount} ${toolCount === 1 ? "tool" : "tools"}) and served from a fast cache, so tool access is much faster. New tools need a new consent.`,
    });
  }

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

function SelectionBox({
  checked,
  indeterminate = false,
}: {
  checked: boolean;
  indeterminate?: boolean;
}): JSX.Element {
  const mixed = indeterminate && !checked;
  return (
    <span
      aria-hidden
      className={cn(
        "border-border flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded-[3px] border",
        (checked || mixed) && "bg-foreground border-foreground",
      )}
    >
      {checked && <Check className="text-background h-2.5 w-2.5" />}
      {mixed && <Minus className="text-background h-2.5 w-2.5" />}
    </span>
  );
}

/** The aria-checked value for a bulk checkbox over a set of tools. */
function bulkChecked(checked: boolean, partial: boolean): boolean | "mixed" {
  if (checked) return true;
  return partial ? "mixed" : false;
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
  catalogMode,
  inventoryID,
  allGrant,
  snapshotAnnotations,
  liveAnnotations,
  tools,
}: {
  formId: string;
  inventoryID: string;
  catalogMode?: "live" | "cached";
  allGrant: boolean;
  snapshotAnnotations: ToolAnnotation[];
  liveAnnotations: ToolAnnotation[];
  tools: string[];
}): JSX.Element {
  const inventoryField = (
    <>
      {catalogMode && (
        <input
          type="hidden"
          name="tool_catalog_mode"
          value={catalogMode}
          form={formId}
        />
      )}
      <input
        type="hidden"
        name="tool_inventory_id"
        value={inventoryID}
        form={formId}
      />
    </>
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
