import { useNoToolsetsConfigured } from "@/hooks/useObservabilityMcpConfig";
import { useServerAssistantTransport } from "@/hooks/useServerAssistantTransport";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { cn, isMacPlatform } from "@/lib/utils";
import speakeasyIcon from "@/assets/speakeasy-icon.svg";
import { useAssistantRuntime } from "@assistant-ui/react";
import type {
  ElementsConfig,
  ElementsTransportFactory,
} from "@gram-ai/elements";
import { Chat, ChatHistory, GramElementsProvider } from "@gram-ai/elements";
import { stripMessageContextFraming } from "@/lib/projectAssistantTranscript";
import {
  INSIGHTS_DOCK_CONTENT_VT_CLASS,
  INSIGHTS_DOCK_VT_CLASS,
  useInsightsDockCta,
} from "@/hooks/useInsightsDockCta";
import { useLocation } from "react-router";
import { motion } from "motion/react";
import {
  getRouteSuggestions,
  INSIGHTS_SUGGESTION_ICONS,
  type InsightsSuggestion,
} from "@/lib/insights-suggestions";
import { useMoonshineConfig } from "@speakeasy-api/moonshine";
import type { UIMessage } from "ai";
import {
  ArrowUp,
  ChevronDown,
  HistoryIcon,
  Loader2,
  Maximize2,
  Minimize2,
  SquarePen,
  Terminal,
  X,
} from "lucide-react";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  ReactElement,
} from "react";
import type { InsightsConfigOptions } from "./insights-context";
import { InsightsContext, useInsightsState } from "./insights-context";
import { useAskAiListener } from "./command-palette/askAiBridge";

// Types-only re-export (erased at compile time, won't break Fast Refresh)
export type { InsightsConfigOptions } from "./insights-context";

/**
 * Cycles an element's `color` through the Speakeasy brand rainbow on hover —
 * used for small icon-only "Explore with AI" CTAs where a border treatment
 * would be invisible. Requires <InsightsRainbowStyles /> in the tree.
 */
export const INSIGHTS_AI_RAINBOW_CLASS = "insights-ai-rainbow";

function InsightsRainbowStyles() {
  return (
    <style>{`
      @keyframes insights-ai-rainbow {
        0%   { color: #C83228; }
        16%  { color: #FB873F; }
        33%  { color: #D2DC91; }
        50%  { color: #5A8250; }
        66%  { color: #2873D7; }
        83%  { color: #9BC3FF; }
        100% { color: #C83228; }
      }
      .${INSIGHTS_AI_RAINBOW_CLASS}:hover {
        animation: insights-ai-rainbow 2.5s linear infinite;
      }
    `}</style>
  );
}

/** Max suggestion chips shown in the dock's expanded state. */
const DOCK_SUGGESTION_LIMIT = 3;

/** Grace period before a blur/outside-click collapse commits. An in-app
 *  navigation landing inside this window keeps the dock expanded — the nav
 *  click itself blurs the input, but the user's intent was to change pages,
 *  not to put the composer away. */
const DOCK_COLLAPSE_GRACE_MS = 350;

/** How recently a grace-timer collapse must have fired for a navigation to
 *  undo it. Covers slow clicks and route transitions that outlast the grace
 *  period (lazy chunks, loaders) — the timer collapses the dock mid-flight
 *  and the location change lands later. Escape and submit collapse without
 *  stamping, so they are never undone. */
const DOCK_REOPEN_WINDOW_MS = 2500;

/** Icon-only buttons in the chat panel's Granola-style header. */
const PANEL_ICON_BUTTON_CLASS =
  "hover:bg-muted text-muted-foreground hover:text-foreground rounded-md p-1.5 transition-colors";

/**
 * Restyles Elements' default chat composer (tall multi-row box with an
 * attachment/mention toolbar) into the same slim rounded-xl single-line row
 * as the docked pill that opens the panel, so the two read as one control.
 * Injected into the Elements shadow root via `theme.customCss` — host-page
 * CSS can't reach it. Targets Elements' stable `aui-*` class hooks; the
 * toolbar is hidden but typing `@` still triggers tool mentions.
 */
const DOCK_PANEL_COMPOSER_CSS = `
  .aui-composer-wrapper { padding-block: 0.5rem; }
  .aui-composer-root { min-height: 0; border-radius: 0.75rem; padding: 0; }
  .aui-composer-input {
    min-height: 0;
    margin-bottom: 0;
    padding: 0.625rem 3rem 0.625rem 1rem;
    font-size: 0.875rem;
    line-height: 1.25rem;
  }
  .aui-composer-action-wrapper {
    position: absolute;
    right: 0.625rem;
    bottom: 50%;
    transform: translateY(50%);
    margin: 0;
  }
  .aui-composer-action-wrapper-inner { display: none; }
  .aui-composer-send, .aui-composer-cancel {
    width: 1.5rem;
    height: 1.5rem;
    min-width: 1.5rem;
    cursor: pointer;
  }
  .aui-composer-send-icon {
    width: 0.875rem;
    height: 0.875rem;
    stroke-width: 2.5;
  }
  /* Filled square reads heavier than the stroked arrow — smaller icon keeps
     visible padding inside the same-size circle. */
  .aui-composer-cancel-icon {
    width: 0.625rem;
    height: 0.625rem;
  }
`;

function DockSubmitButton() {
  return (
    <button
      type="submit"
      aria-label="Send to Project Assistant"
      className="bg-primary text-primary-foreground hover:bg-primary/90 flex size-6 shrink-0 items-center justify-center rounded-full transition-colors"
    >
      <ArrowUp className="size-3.5" />
    </button>
  );
}

interface InsightsDockProps {
  /** Effective suggestion prompts (page override or provider defaults). */
  suggestions: InsightsSuggestion[];
  /** True while the chat panel is open — the composer row collapses and the
   *  panel grows up out of the dock card in its place. */
  open: boolean;
  /** Bumped by the provider when the keyboard shortcut fires while the
   *  panel is closed; the dock responds by focusing (and therefore
   *  expanding) the input. Starts at 0 — the initial value is ignored. */
  focusKey: number;
  onSubmitPrompt: (text: string) => void;
  /** Genies the dock down into the sidebar-footer resume button. */
  onDismiss: () => void;
  /** Chat panel content, rendered inside the card when `open`. */
  panel: React.ReactNode;
  /** Stretch the open chat panel to fill the content area. */
  maximized: boolean;
}

/** Width/shape/elevation of the dock card across its states: chat panel
 *  full-screen, chat panel open, composer focused (or holding draft text),
 *  and collapsed pill. Activity is signalled by deepening shadow. */
function dockCardShapeClass(
  open: boolean,
  composerExpanded: boolean,
  maximized: boolean,
): string {
  if (open && maximized) return "h-full max-w-none rounded-2xl shadow-2xl";
  if (open) return "max-w-3xl rounded-2xl shadow-2xl";
  if (composerExpanded) return "max-w-2xl rounded-2xl shadow-xl";
  return "max-w-md rounded-full shadow-md hover:shadow-lg";
}

/**
 * Permanently docked composer for the Project Assistant, floating at the
 * bottom-center of the content area as a compact "Ask anything" pill.
 *
 * Expand behavior: focusing the input (click, tab, or Cmd+/) expands the
 * card upward — it is bottom-anchored, so revealing the suggestion row via
 * a `grid-template-rows: 0fr -> 1fr` transition grows the card up, while
 * max-width eases out at the same time. Draft text keeps it expanded even
 * when focus moves away so a stray click never hides what the user typed.
 *
 * Submit behavior (Enter or a suggestion chip): the card itself expands
 * into the full chat panel — the composer row collapses (the chat brings
 * its own composer) and `panel` grows up out of the dock via the same
 * grid-rows transition. No separate sidebar is involved.
 */
function InsightsDock({
  suggestions,
  open,
  focusKey,
  onSubmitPrompt,
  onDismiss,
  panel,
  maximized,
}: InsightsDockProps): ReactElement {
  const [value, setValue] = useState("");
  // Expansion is sticky state, not a focus mirror: it must survive the input
  // losing focus to an in-app navigation so the new page's suggestions show
  // without re-opening the dock. Collapses commit through a grace timer that
  // a route change can cancel (or, for slow clicks, undo).
  const [expanded, setExpanded] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const cardRef = useRef<HTMLDivElement>(null);
  const collapseTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Stamped only by grace-timer collapses (blur/outside click), never by
  // Escape or submit — only ambient collapses are undone by a navigation.
  const collapsedAt = useRef(0);
  const location = useLocation();

  const cancelCollapse = useCallback(() => {
    if (collapseTimer.current !== null) {
      clearTimeout(collapseTimer.current);
      collapseTimer.current = null;
    }
  }, []);

  const scheduleCollapse = useCallback(() => {
    cancelCollapse();
    collapseTimer.current = setTimeout(() => {
      collapseTimer.current = null;
      collapsedAt.current = Date.now();
      setExpanded(false);
    }, DOCK_COLLAPSE_GRACE_MS);
  }, [cancelCollapse]);

  /** Deliberate collapse (Escape, submit): immediate and never undone. */
  const collapseNow = useCallback(() => {
    cancelCollapse();
    collapsedAt.current = 0;
    setExpanded(false);
  }, [cancelCollapse]);

  useEffect(() => cancelCollapse, [cancelCollapse]);

  // Keyboard shortcut path: the provider bumps focusKey when Cmd+/ fires.
  useEffect(() => {
    if (focusKey === 0) return;
    inputRef.current?.focus();
  }, [focusKey]);

  // Navigation keeps the dock expanded: cancel any pending collapse, and undo
  // one that just fired (slow click — the grace timer can win the race
  // against mouseup). The new page's suggestions render into the already-open
  // suggestion row.
  const isFirstLocation = useRef(true);
  useEffect(() => {
    if (isFirstLocation.current) {
      isFirstLocation.current = false;
      return;
    }
    cancelCollapse();
    if (Date.now() - collapsedAt.current < DOCK_REOPEN_WINDOW_MS) {
      collapsedAt.current = 0;
      setExpanded(true);
    }
  }, [location.pathname, cancelCollapse]);

  // After surviving a navigation the dock is expanded without holding focus,
  // so blur can never fire again — collapse on pointerdown outside the card
  // instead. Goes through the grace timer so a click on another nav link
  // still keeps it open.
  useEffect(() => {
    if (!expanded) return;
    const onPointerDown = (e: PointerEvent) => {
      if (cardRef.current?.contains(e.target as Node)) return;
      scheduleCollapse();
    };
    document.addEventListener("pointerdown", onPointerDown);
    return () => document.removeEventListener("pointerdown", onPointerDown);
  }, [expanded, scheduleCollapse]);

  const composerExpanded = !open && (expanded || value.trim().length > 0);

  const shortcutAria = isMacPlatform() ? "Meta+/" : "Control+/";

  const submit = (text: string) => {
    const trimmed = text.trim();
    if (!trimmed) return;
    setValue("");
    inputRef.current?.blur();
    // Deliberate collapse so closing the chat panel later restores the pill,
    // not a lingering expanded composer.
    collapseNow();
    onSubmitPrompt(trimmed);
  };

  return (
    <div
      className={cn(
        "pointer-events-none absolute inset-x-0 bottom-0 z-30 flex justify-center px-4 pt-14 pb-8",
        // Full-screen: anchor the wrapper to the whole content area so the
        // card (h-full) fills it, keeping the floating-card margins.
        open && maximized && "top-0 pt-8",
      )}
    >
      {/* Frosted veil under the dock: blurs the page content directly behind
          the card, radiating out as a soft ellipse centred on the dock so
          there is no rectangular edge. Pointer-events stay off so the page
          remains clickable. */}
      <div
        aria-hidden="true"
        className="absolute inset-0 backdrop-blur-[2px] [mask-image:radial-gradient(ellipse_55%_95%_at_50%_100%,black_35%,transparent_78%)]"
      />
      <div
        className={cn(
          "border-border bg-card text-card-foreground pointer-events-auto w-full border",
          "transition-all duration-300 ease-out",
          dockCardShapeClass(open, composerExpanded, maximized),
          // Pairs with the sidebar resume button for the dismiss/resume genie
          // (see useInsightsDockCta). The inner wrapper carries the content
          // name so text fades at the endpoints instead of warping mid-flight.
          INSIGHTS_DOCK_VT_CLASS,
        )}
        ref={cardRef}
        // Expand only when the INPUT gains focus — not any child. Expanding
        // on e.g. the dismiss button's focus would shift the card's layout
        // mid-click (mousedown focuses, the button slides out from under the
        // cursor, mouseup misses, the click never lands). Blur bubbles from
        // all children; collapse only when focus leaves the card entirely —
        // and even then through the grace timer, so a navigation click keeps
        // the dock expanded for the next page's suggestions.
        onFocus={(e) => {
          if (e.target === inputRef.current) {
            cancelCollapse();
            setExpanded(true);
          }
        }}
        onBlur={(e) => {
          if (!e.currentTarget.contains(e.relatedTarget as Node | null)) {
            scheduleCollapse();
          }
        }}
      >
        <div
          className={cn(
            INSIGHTS_DOCK_CONTENT_VT_CLASS,
            // Full-screen: the card is h-full, so switch the content wrapper
            // to a column flex and let the panel row absorb the height.
            open && maximized && "flex h-full flex-col",
          )}
        >
          {/* Chat panel — grows up out of the dock when open. The 0fr->1fr grid
            row transition animates from zero height to the panel's fixed
            height without hardcoding it on the card; since the card is
            bottom-anchored the growth reads as upward expansion. */}
          <div
            aria-hidden={!open}
            inert={!open}
            className={cn(
              "grid grid-rows-[0fr] transition-[grid-template-rows] duration-300 ease-out",
              open && "grid-rows-[1fr]",
              open && maximized && "min-h-0 flex-1",
            )}
          >
            <div
              className={cn("overflow-hidden", open && maximized && "h-full")}
            >
              {/* Same grey-tray-around-white-surface treatment as the
                  expanded composer, so opening the chat reads as the input
                  surface growing into the conversation rather than an
                  unrelated white panel appearing. */}
              <div
                className={cn(
                  "bg-muted/40 rounded-2xl p-2",
                  open && maximized && "h-full",
                )}
              >
                <div
                  className={cn(
                    "border-border bg-card h-[min(640px,70vh)] overflow-hidden rounded-xl border",
                    open && maximized && "h-full",
                  )}
                >
                  {panel}
                </div>
              </div>
            </div>
          </div>

          {/* Composer — collapses while the panel is open since the chat
            brings its own composer at the same bottom-anchored position. */}
          <div
            aria-hidden={open}
            inert={open}
            className={cn(
              "grid grid-rows-[1fr] transition-[grid-template-rows] duration-300 ease-out",
              open && "grid-rows-[0fr]",
            )}
          >
            <div className="overflow-hidden">
              {/* Granola-style expanded composer: the outer card gains inset
                padding, the chip row sits at the top, and the input row gets
                its own bordered rounded container. Collapsed, the padding and
                inner border melt away so the pill reads as a single surface
                (border-transparent keeps the box metrics identical). */}
              <div
                className={cn(
                  // Tint sits over the card's solid bg-card (rather than
                  // replacing it) so the tray reads as a subtle grey without
                  // page content bleeding through the translucency.
                  "rounded-2xl transition-[padding,background-color] duration-300 ease-out",
                  composerExpanded ? "bg-muted/40 p-2" : "p-0",
                )}
              >
                {/* Suggestion chips — revealed above the input on focus, same
                  grid-rows technique. */}
                {suggestions.length > 0 && (
                  <div
                    aria-hidden={!composerExpanded}
                    className={cn(
                      "grid grid-rows-[0fr] opacity-0 transition-[grid-template-rows,opacity] duration-300 ease-out",
                      composerExpanded && "grid-rows-[1fr] opacity-100",
                    )}
                  >
                    <div className="overflow-hidden">
                      {/* Chips are keyed by title: a route change swaps the
                          old set out instantly (no exit animation — exiting
                          ghosts looked like artifacts) and staggers the new
                          chips in. A chip shared between pages keeps its
                          element and doesn't re-animate. */}
                      <div className="flex flex-wrap items-center gap-1.5 px-2.5 pt-1 pb-2.5">
                        {suggestions
                          .slice(0, DOCK_SUGGESTION_LIMIT)
                          .map((suggestion, index) => {
                            const SuggestionIcon =
                              INSIGHTS_SUGGESTION_ICONS[
                                suggestion.icon ?? "sparkles"
                              ];
                            return (
                              <motion.button
                                key={suggestion.title}
                                initial={{ opacity: 0, y: 10, scale: 0.95 }}
                                animate={{
                                  opacity: 1,
                                  y: 0,
                                  scale: 1,
                                  transition: {
                                    delay: index * 0.06,
                                    duration: 0.25,
                                    ease: "easeOut",
                                  },
                                }}
                                type="button"
                                tabIndex={composerExpanded ? 0 : -1}
                                onClick={() => submit(suggestion.prompt)}
                                className="border-border bg-card text-muted-foreground hover:bg-accent hover:text-accent-foreground flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs transition-colors"
                              >
                                <SuggestionIcon className="size-3 shrink-0" />
                                {suggestion.title}
                              </motion.button>
                            );
                          })}
                      </div>
                    </div>
                  </div>
                )}
                <form
                  onSubmit={(e) => {
                    e.preventDefault();
                    submit(value);
                  }}
                  className={cn(
                    "flex items-center gap-2.5 rounded-xl border px-4 py-2.5 transition-colors duration-300 ease-out",
                    composerExpanded
                      ? "border-border bg-card"
                      : "border-transparent bg-transparent",
                  )}
                >
                  <input
                    ref={inputRef}
                    value={value}
                    onChange={(e) => setValue(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === "Escape") {
                        collapseNow();
                        inputRef.current?.blur();
                      }
                    }}
                    tabIndex={open ? -1 : 0}
                    placeholder="Ask anything"
                    aria-label="Ask the Project Assistant"
                    aria-keyshortcuts={shortcutAria}
                    className="placeholder:text-muted-foreground min-w-0 flex-1 bg-transparent text-sm outline-none"
                  />
                  {value.trim() && <DockSubmitButton />}
                  <button
                    type="button"
                    onClick={onDismiss}
                    tabIndex={open ? -1 : 0}
                    className={PANEL_ICON_BUTTON_CLASS}
                    aria-label="Dismiss the assistant dock"
                    title="Dismiss"
                  >
                    <X className="size-3.5" />
                  </button>
                </form>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

/**
 * Page-level config override. Mount this anywhere inside an InsightsProvider
 * to swap in a custom prompt/suggestions/MCP filter. Cleans up on unmount,
 * restoring the provider's defaults.
 */
export function InsightsConfig(
  options: InsightsConfigOptions,
): ReactElement | null {
  const { setOverride } = useInsightsState();
  // JSON.stringify is the stable content key; optionsRef is read inside the
  // effect to avoid a stale closure on re-renders that don't change content.
  const optionsRef = useRef(options);
  optionsRef.current = options;
  const key = JSON.stringify(options);
  useEffect(() => {
    setOverride(optionsRef.current);
    return () => setOverride(null);
  }, [key, setOverride]);
  return null;
}

interface InsightsProviderProps {
  /** Default MCP config used when no <InsightsConfig> override is mounted. */
  mcpConfig: Omit<ElementsConfig, "variant" | "welcome" | "theme">;
  /** Default welcome title. */
  title: string;
  /** Default welcome subtitle. */
  subtitle: string;
  /** Default suggestion prompts. */
  suggestions?: InsightsSuggestion[];
  /** Default expanded state. */
  defaultExpanded?: boolean;
  /** Children rendered alongside the dock (page content). */
  children: React.ReactNode;
}

export function InsightsProvider({
  mcpConfig: defaultMcpConfig,
  title: defaultTitle,
  subtitle: defaultSubtitle,
  suggestions: defaultSuggestions = [],
  defaultExpanded = false,
  children,
}: InsightsProviderProps): ReactElement {
  const [isExpanded, setIsExpanded] = useState(defaultExpanded);
  const [override, setOverride] = useState<InsightsConfigOptions | null>(null);
  // Bumped whenever the keyboard shortcut fires (with the chat panel closed) so
  // the docked composer grabs focus and expands. Starts at 0; the dock
  // ignores the initial value.
  const [focusComposerKey, setFocusComposerKey] = useState(0);
  const [pendingPrompt, setPendingPrompt] = useState<{
    text: string;
    nonce: number;
  } | null>(null);
  // Used as React `key` on <GramElementsProvider>; bumped from
  // handleSendPrompt so each "Explore with AI" click gets a fresh assistant
  // runtime. Avoids an assistant-ui race where rapid switchToNewThread()
  // calls in a long-lived runtime throw `tapLookupResources: Resource not
  // found for lookup: __LOCALID_…` during render.
  const [sessionKey, setSessionKey] = useState(0);
  const [historyOpen, setHistoryOpen] = useState(false);
  // Full-screen chat panel, toggled from the panel header. Reset on close so
  // the next open always starts at the regular panel size.
  const [panelMaximized, setPanelMaximized] = useState(false);
  useEffect(() => {
    if (!isExpanded) setPanelMaximized(false);
  }, [isExpanded]);
  const { theme } = useMoonshineConfig();
  const { pathname } = useLocation();

  // Resolve effective values: per-page override wins, then the colocated
  // route-level suggestions (lib/insights-suggestions.ts), then the
  // provider's defaults. The route fallback means every page gets relevant
  // suggestions without mounting an <InsightsConfig>.
  const mcpConfig = override?.mcpConfig ?? defaultMcpConfig;
  const title = override?.title ?? defaultTitle;
  const subtitle = override?.subtitle ?? defaultSubtitle;
  const routeSuggestions = useMemo(
    () => getRouteSuggestions(pathname),
    [pathname],
  );
  const suggestions =
    override?.suggestions ?? routeSuggestions ?? defaultSuggestions;
  const contextInfo = override?.contextInfo;
  const hideTrigger = override?.hideTrigger ?? false;
  const noToolsetsConfigured = useNoToolsetsConfigured(mcpConfig.projectSlug);

  // Server-side Project Assistant. Resolved lazily once the chat panel is first
  // opened. While connecting (or after a failure) the factory is undefined and
  // we gate the chat (below) instead of falling back to client-side generation.
  // assistantId scopes the conversation list to this assistant's chats.
  const {
    transport: serverTransport,
    assistantId: managedAssistantId,
    ready: assistantReady,
    error: assistantError,
  } = useServerAssistantTransport(mcpConfig.projectSlug, isExpanded);

  // Read inside the transport wrapper via ref so override churn doesn't
  // re-create the transport identity on every parent re-render.
  const contextInfoRef = useRef(contextInfo);
  contextInfoRef.current = contextInfo;

  // Wrap the server transport so the dashboard context (date range, chart
  // identity for "Explore with AI" clicks) reaches the model. The server owns
  // the Project Assistant's system prompt, so we can't inject the context
  // there — instead we prepend it to the outgoing user message text in a
  // tagged block. The wrapper only modifies what's sent over the wire; the
  // user's message bubble in the UI is already in the assistant-ui store and
  // is unaffected.
  const wrappedTransport = useMemo<ElementsTransportFactory | undefined>(() => {
    if (!serverTransport) return undefined;
    return (ctx) => {
      const inner = serverTransport(ctx);
      return {
        sendMessages: async (args) => {
          const ctxText = contextInfoRef.current;
          if (!ctxText) {
            return inner.sendMessages(args);
          }
          // Find the latest user message and prepend the context to its text
          // parts. Clone shallowly so the array passed to assistant-ui's
          // optimistic store is left intact.
          const messages = args.messages;
          let lastUserIdx = -1;
          for (let i = messages.length - 1; i >= 0; i--) {
            if (messages[i]!.role === "user") {
              lastUserIdx = i;
              break;
            }
          }
          if (lastUserIdx === -1) {
            return inner.sendMessages(args);
          }
          const original = messages[lastUserIdx];
          const prefix = `<dashboard_context>\n${ctxText}\n</dashboard_context>\n\n`;
          let prefixed = false;
          const newParts = original!.parts.map((p) => {
            if (!prefixed && p.type === "text") {
              prefixed = true;
              return { ...p, text: `${prefix}${p.text}` };
            }
            return p;
          });
          // If the user message has no text part (pure image/audio/attachment),
          // skip the prepend entirely — fabricating a text-only part would turn
          // the send into a context-only prompt with no actual user question.
          if (!prefixed) {
            return inner.sendMessages(args);
          }
          const wrappedMessages: UIMessage[] = [
            ...messages.slice(0, lastUserIdx),
            { ...original, parts: newParts } as UIMessage,
            ...messages.slice(lastUserIdx + 1),
          ];
          return inner.sendMessages({ ...args, messages: wrappedMessages });
        },
        reconnectToStream: inner.reconnectToStream.bind(inner),
      };
    };
  }, [serverTransport]);

  const elementsConfig = useMemo<ElementsConfig>(
    () => ({
      ...mcpConfig,
      variant: "standalone",
      // Route the conversation through the persistent server-side Project
      // Assistant. Its model and system prompt are owned server-side, so we
      // don't set them here. `wrappedTransport` is the server transport with a
      // thin wrapper that inlines the per-page dashboard context (date range,
      // chart identity) into outgoing user messages — see `wrappedTransport`
      // above.
      transport: wrappedTransport,
      // Edit relies on assistant-ui's local branch rewriting, which the
      // server-side assistant transport can't honour — hide the affordance
      // rather than ship a control that silently no-ops.
      allowMessageEdit: false,
      // History, the conversation list, and titles come from the chat service
      // via Elements' thread-list adapter, scoped to this assistant's chats. The
      // assistant mints chat ids server-side, so defer client-side id minting.
      history: {
        enabled: true,
        threadListFilters: { assistant_id: managedAssistantId },
        deferThreadIdMinting: true,
        // The runtime persists each turn with a backend `<message-context>`
        // framing block (needed for replay, noise for display). Strip it — and
        // drop framing-only turns — before Elements renders the transcript.
        transformChatMessage: stripMessageContextFraming,
      },
      api: {
        ...mcpConfig.api,
        headers: {
          ...mcpConfig.api?.headers,
          "X-Gram-Source": "dashboard-ai-insights",
        },
      },
      welcome: {
        logo: speakeasyIcon,
        title,
        subtitle,
        suggestions,
      },
      // Mirror the docked pill's wording so the chat composer reads as the
      // same control the user just typed into. Attachments are hidden — the
      // dock has no attach affordance and the feature is not implemented.
      composer: {
        placeholder: "Ask anything",
        attachments: false,
      },
      theme: {
        colorScheme: theme === "dark" ? "dark" : "light",
        customCss: DOCK_PANEL_COMPOSER_CSS,
      },
    }),
    [
      mcpConfig,
      title,
      subtitle,
      suggestions,
      theme,
      wrappedTransport,
      managedAssistantId,
    ],
  );

  // Page-level <InsightsConfig> calls this on every parent re-render and on
  // page navigation; deliberately does NOT bump sessionKey, so navigating
  // between pages preserves any in-flight chat.
  const handleSetOverride = useCallback(
    (next: InsightsConfigOptions | null) => {
      setOverride(next);
    },
    [],
  );

  // Only "Explore with AI" clicks call this — bump sessionKey here (not in
  // setOverride) so a fresh runtime is mounted before the prompt lands.
  // Nonce defeats reference-equality skipping when the same chart is clicked
  // twice in a row.
  const handleSendPrompt = useCallback((text: string) => {
    setSessionKey((k) => k + 1);
    setPendingPrompt({ text, nonce: Date.now() });
  }, []);

  const consumePendingPrompt = useCallback(() => setPendingPrompt(null), []);

  // Bridge: the command palette's "Ask AI" row dispatches a window event from
  // outside this provider. Open the chat panel and, when a prompt was typed,
  // drop the user straight into a running conversation.
  useAskAiListener(
    useCallback(
      (prompt: string) => {
        setIsExpanded(true);
        if (prompt.trim()) handleSendPrompt(prompt);
      },
      [handleSendPrompt],
    ),
  );

  // Submissions from the docked composer follow the same path as the command
  // palette: open the chat panel and drop the prompt into a fresh
  // conversation.
  const handleDockSubmit = useCallback(
    (text: string) => {
      setIsExpanded(true);
      handleSendPrompt(text);
    },
    [handleSendPrompt],
  );

  // Dismiss genies the dock down into the sidebar-footer resume button (and
  // the button genies it back) — same view transition as the onboarding
  // banner. Collapse the panel too so a resume always restores the pill.
  const { dismissed: dockDismissed, dismiss: dismissDock } =
    useInsightsDockCta();
  const handleDockDismiss = useCallback(() => {
    setIsExpanded(false);
    dismissDock();
  }, [dismissDock]);

  // Start a brand-new Project Assistant conversation: remount the chat provider
  // (bumping sessionKey) so a fresh thread opens. With server-side id minting,
  // the new thread gets its chat id from the server on the first send.
  const handleStartFresh = useCallback(() => {
    setSessionKey((k) => k + 1);
  }, []);

  // Global keyboard shortcut: Cmd+/ (Mac) / Ctrl+/ (PC). With the chat panel
  // closed it focuses the docked composer; with it open it collapses the
  // panel back into the composer pill.
  //
  // Skips when the dock is hidden via `hideTrigger`, when a modifier
  // mismatch is detected (extra Alt/Shift), or when the user is typing in a
  // contentEditable region — letting Cmd+/ still work in plain inputs since
  // the Cmd/Ctrl modifier means it never inserts text.
  useEffect(() => {
    if (hideTrigger) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (!e.metaKey && !e.ctrlKey) return;
      if (e.altKey) return;
      // KeyboardEvent.code is layout-independent, unlike e.key which varies
      // with keyboard layout and held modifiers.
      if (e.code !== "Slash") return;
      const target = e.target as HTMLElement | null;
      if (target?.isContentEditable) return;
      e.preventDefault();
      if (isExpanded) {
        setIsExpanded(false);
      } else {
        setFocusComposerKey((k) => k + 1);
      }
    };
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [hideTrigger, isExpanded]);

  const contextValue = useMemo(
    () => ({
      available: !hideTrigger,
      isExpanded,
      setIsExpanded,
      setOverride: handleSetOverride,
      sendPrompt: handleSendPrompt,
    }),
    [hideTrigger, isExpanded, handleSetOverride, handleSendPrompt],
  );

  // Shared between the ready and connecting headers.
  const panelCloseButton = (
    <button
      onClick={() => setIsExpanded(false)}
      className={PANEL_ICON_BUTTON_CLASS}
      aria-label="Close Project Assistant"
      title="Close"
    >
      <X className="size-4" />
    </button>
  );

  const panelNotices = (
    <>
      {/* Notice when the Project Assistant failed to connect */}
      {assistantError && (
        <div className="border-destructive/40 bg-destructive/10 text-destructive mx-4 mt-1 flex items-start gap-2 rounded-md border px-3 py-2 text-xs">
          <Terminal className="mt-0.5 size-3.5 shrink-0" />
          <span>{assistantError}</span>
        </div>
      )}

      {/* Notice when no toolsets are configured */}
      {noToolsetsConfigured && (
        <div className="border-border bg-muted/50 text-muted-foreground mx-4 mt-1 flex items-start gap-2 rounded-md border px-3 py-2 text-xs">
          <Terminal className="mt-0.5 size-3.5 shrink-0" />
          <span>
            AI tools are unavailable. Create an MCP server to enable the Project
            Assistant.
          </span>
        </div>
      )}
    </>
  );

  // Expanded chat panel rendered inside the dock card. Granola-style header:
  // icon-only controls with no title bar — history picker on the left; new
  // conversation, full assistants page, and close on the right. The whole
  // header lives inside the Elements provider (when ready) because
  // <ChatHistory> needs the chat runtime; "New" remounts it via sessionKey,
  // which is fine since the header holds no local state.
  const panelContent = (
    <div className="flex h-full flex-col">
      {assistantReady ? (
        <GramElementsProvider key={sessionKey} config={elementsConfig}>
          <PendingPromptBridge
            pending={pendingPrompt}
            onConsume={consumePendingPrompt}
          />
          <div className="flex h-full min-h-0 flex-col">
            {/* Header */}
            <div className="flex shrink-0 items-center justify-between px-2 pt-2">
              <Popover open={historyOpen} onOpenChange={setHistoryOpen}>
                <PopoverTrigger asChild>
                  <button
                    className={cn(
                      PANEL_ICON_BUTTON_CLASS,
                      "flex items-center gap-0.5",
                    )}
                    aria-label="Conversation history"
                    title="Conversation history"
                  >
                    <HistoryIcon className="size-4" />
                    <ChevronDown className="size-3" />
                  </button>
                </PopoverTrigger>
                <PopoverContent
                  align="start"
                  className="max-h-96 w-72 overflow-y-auto p-0"
                >
                  <ChatHistory className="max-h-96 overflow-y-auto" />
                </PopoverContent>
              </Popover>
              <div className="flex items-center gap-0.5">
                <button
                  onClick={handleStartFresh}
                  className={PANEL_ICON_BUTTON_CLASS}
                  aria-label="Start a new conversation"
                  title="Start a new conversation"
                >
                  <SquarePen className="size-4" />
                </button>
                <button
                  onClick={() => setPanelMaximized((m) => !m)}
                  className={PANEL_ICON_BUTTON_CLASS}
                  aria-label={
                    panelMaximized ? "Exit full screen" : "Full screen"
                  }
                  title={panelMaximized ? "Exit full screen" : "Full screen"}
                >
                  {panelMaximized ? (
                    <Minimize2 className="size-4" />
                  ) : (
                    <Maximize2 className="size-4" />
                  )}
                </button>
                {panelCloseButton}
              </div>
            </div>
            {panelNotices}
            <div className="min-h-0 flex-1 overflow-hidden">
              <Chat />
            </div>
          </div>
        </GramElementsProvider>
      ) : (
        <>
          {/* Header (connecting state) — close only; history/new need the
              chat runtime. */}
          <div className="flex shrink-0 items-center justify-end px-2 pt-2">
            {panelCloseButton}
          </div>
          {panelNotices}
          {!assistantError && (
            <div className="text-muted-foreground flex flex-1 items-center justify-center gap-2 text-sm">
              <Loader2 className="size-4 animate-spin" />
              <span>Connecting to the Project Assistant…</span>
            </div>
          )}
        </>
      )}
    </div>
  );

  return (
    <InsightsContext.Provider value={contextValue}>
      <InsightsRainbowStyles />
      {/* Relative so the docked composer floats at the bottom-center of the
          content area. */}
      <div className="relative h-full w-full overflow-hidden">
        {children}

        {/* Backdrop overlay - closes the chat panel when clicked */}
        {isExpanded && (
          <div
            className="fixed inset-0 z-20"
            onClick={() => setIsExpanded(false)}
            aria-hidden="true"
          />
        )}

        {/* Permanently docked "Ask anything" composer — the entry point to
            the Project Assistant. Expands in place into the chat panel.
            Hidden on pages that opt out via hideTrigger, and while dismissed
            to the sidebar resume button. */}
        {!hideTrigger && !dockDismissed && (
          <InsightsDock
            suggestions={suggestions}
            open={isExpanded}
            focusKey={focusComposerKey}
            onSubmitPrompt={handleDockSubmit}
            onDismiss={handleDockDismiss}
            panel={panelContent}
            maximized={panelMaximized}
          />
        )}
      </div>
    </InsightsContext.Provider>
  );
}

/**
 * Lives inside GramElementsProvider so it can access useThreadRuntime().
 * When a pending prompt is queued via InsightsContext.sendPrompt, this bridge
 * appends it to the current thread as a user message, triggering the
 * assistant to respond. It fires once per nonce so repeat clicks on the
 * same CTA still work.
 */
function PendingPromptBridge({
  pending,
  onConsume,
}: {
  pending: { text: string; nonce: number } | null;
  onConsume: () => void;
}) {
  const assistantRuntime = useAssistantRuntime();
  const firedNonceRef = useRef<number | null>(null);

  useEffect(() => {
    if (!pending || !assistantRuntime) return;
    if (firedNonceRef.current === pending.nonce) return;
    firedNonceRef.current = pending.nonce;

    const { text } = pending;

    // On a cold open the runtime mounts while its remote thread-list adapter
    // is still initialising (thread state `isLoading`), and an append fired
    // into that window is silently dropped. Append only once the thread has
    // finished loading, verify the optimistic user message actually landed,
    // and otherwise wait for the next runtime event and try again.
    let done = false;
    let unsubscribe: (() => void) | null = null;

    const attempt = () => {
      if (done) return;
      const state = assistantRuntime.thread.getState();
      if (state.isLoading || state.isDisabled) return;
      try {
        assistantRuntime.thread.append(text);
      } catch (err) {
        console.error("Failed to send queued assistant prompt:", err);
        return;
      }
      if (assistantRuntime.thread.getState().messages.length === 0) return;
      done = true;
      unsubscribe?.();
      unsubscribe = null;
      onConsume();
    };

    attempt();
    if (!done) {
      unsubscribe = assistantRuntime.thread.subscribe(attempt);
    }

    return () => {
      done = true;
      unsubscribe?.();
      unsubscribe = null;
    };
  }, [pending, assistantRuntime, onConsume]);

  return null;
}
