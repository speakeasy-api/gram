import { useNoToolsetsConfigured } from "@/hooks/useObservabilityMcpConfig";
import { useServerAssistantTransport } from "@/hooks/useServerAssistantTransport";
import { cn } from "@/lib/utils";
import { useAssistantRuntime } from "@assistant-ui/react";
import type {
  ElementsConfig,
  ElementsTransportFactory,
} from "@gram-ai/elements";
import { Chat, GramElementsProvider } from "@gram-ai/elements";
import { useMoonshineConfig } from "@speakeasy-api/moonshine";
import type { UIMessage } from "ai";
import {
  ChevronRight,
  Loader2,
  Sparkles,
  SquarePen,
  Terminal,
  Wand2,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { InsightsConfigOptions } from "./insights-context";
import { InsightsContext, useInsightsState } from "./insights-context";

// Types-only re-export (erased at compile time, won't break Fast Refresh)
export type { InsightsConfigOptions } from "./insights-context";

/**
 * Cycles an element's `color` through the Speakeasy brand rainbow on hover —
 * used for small icon-only "Explore with AI" CTAs where a border treatment
 * would be invisible. Requires <InsightsRainbowStyles /> in the tree.
 */
export const INSIGHTS_AI_RAINBOW_CLASS = "insights-ai-rainbow";

/**
 * Reveals a full-spectrum Speakeasy brand gradient border on hover — same
 * 9-stop palette as the login page's BrandGradientBar. Used for the nav-bar
 * AI Insights trigger where the button shape can host a real border.
 * Requires <InsightsRainbowStyles /> in the tree. Works best on elements
 * with a 1px border and a border-radius.
 */
export const INSIGHTS_AI_RAINBOW_BORDER_CLASS = "insights-ai-rainbow-border";

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

      /* One-shot spin used when the trigger is clicked (or fired via the
         keyboard shortcut). Single iteration so the icon settles back to
         rest position; the class is removed after the timeout in JS. */
      @keyframes insights-trigger-spin {
        from { transform: rotate(0deg); }
        to   { transform: rotate(360deg); }
      }
      .insights-trigger-spinning {
        animation: insights-trigger-spin 600ms ease-in-out;
      }

      /* Gradient border via a masked pseudo-element. Mask cuts the interior
         so only the 1px ring shows; border-radius is inherited so rounded
         corners stay intact. Fades in on hover; the underlying border goes
         transparent so we don't double up. */
      .${INSIGHTS_AI_RAINBOW_BORDER_CLASS} {
        position: relative;
      }
      .${INSIGHTS_AI_RAINBOW_BORDER_CLASS}::before {
        content: "";
        position: absolute;
        inset: 0;
        padding: 1px;
        border-radius: inherit;
        background: linear-gradient(90deg, #320F1E 0%, #C83228 12.5%, #FB873F 25%, #D2DC91 37.5%, #5A8250 50%, #002314 62%, #00143C 74%, #2873D7 86%, #9BC3FF 100%);
        -webkit-mask: linear-gradient(#fff 0 0) content-box, linear-gradient(#fff 0 0);
        -webkit-mask-composite: xor;
        mask: linear-gradient(#fff 0 0) content-box, linear-gradient(#fff 0 0);
        mask-composite: exclude;
        opacity: 0;
        transition: opacity 200ms ease;
        pointer-events: none;
      }
      .${INSIGHTS_AI_RAINBOW_BORDER_CLASS}:hover::before {
        opacity: 1;
      }
      .${INSIGHTS_AI_RAINBOW_BORDER_CLASS}:hover {
        border-color: transparent;
      }
    `}</style>
  );
}

/** Single source of truth for the trigger's keyboard shortcut. Cmd+W is
 *  reserved by the browser (closes the tab before JS sees the event), so we
 *  use Option+Shift+W which matches common app conventions and mirrors the
 *  reference design. */
const INSIGHTS_SHORTCUT_LABEL_MAC = ["⌥", "⇧", "W"]; // ⌥ ⇧ W
const INSIGHTS_SHORTCUT_LABEL_PC = ["Alt", "Shift", "W"];

function isMacPlatform(): boolean {
  if (typeof navigator === "undefined") return true;
  return /mac|iphone|ipad|ipod/i.test(
    navigator.platform || navigator.userAgent,
  );
}

/**
 * Header-bar trigger for opening the AI Insights sidebar. Renders only
 * when inside an InsightsProvider so it can be slotted globally (e.g. into
 * PageHeaderBreadcrumbs) without appearing on pages that opt out via
 * hideTrigger.
 *
 * Hover behavior: the kbd hint is collapsed to zero width by default and
 * expands via a CSS grid `grid-template-columns: 0fr -> 1fr` transition on
 * hover. The button is anchored to the right edge of the page header, so
 * the growth pushes the left edge outward — matching the reference design.
 *
 * Click / shortcut behavior: when the trigger fires (mouse click or the
 * `triggerSpinKey` from the provider increments), the wand icon plays a
 * single 600ms rotation. Implemented by mounting/unmounting the spin class
 * with a setTimeout rather than `animate-spin` (which is infinite).
 */
export function InsightsTrigger({ className }: { className?: string }) {
  const { available, isExpanded, setIsExpanded, triggerSpinKey } =
    useInsightsState();
  const [spinning, setSpinning] = useState(false);
  const spinTimeoutRef = useRef<number | null>(null);

  const startSpin = useCallback(() => {
    if (spinTimeoutRef.current !== null) {
      window.clearTimeout(spinTimeoutRef.current);
    }
    setSpinning(true);
    spinTimeoutRef.current = window.setTimeout(() => {
      setSpinning(false);
      spinTimeoutRef.current = null;
    }, 600);
  }, []);

  // Re-trigger spin whenever the provider bumps `triggerSpinKey` — this fires
  // for the keyboard shortcut path so the icon animates even when the click
  // didn't originate from this button.
  useEffect(() => {
    if (triggerSpinKey === 0) return;
    startSpin();
  }, [triggerSpinKey, startSpin]);

  // Clean up the timeout if the trigger unmounts mid-spin.
  useEffect(
    () => () => {
      if (spinTimeoutRef.current !== null) {
        window.clearTimeout(spinTimeoutRef.current);
      }
    },
    [],
  );

  const shortcutKeys = isMacPlatform()
    ? INSIGHTS_SHORTCUT_LABEL_MAC
    : INSIGHTS_SHORTCUT_LABEL_PC;
  const shortcutAria = isMacPlatform() ? "Option Shift W" : "Alt Shift W";

  if (!available) return null;

  return (
    <button
      type="button"
      onClick={() => {
        startSpin();
        setIsExpanded(!isExpanded);
      }}
      aria-label={isExpanded ? "Close AI Insights" : "Open AI Insights"}
      aria-keyshortcuts={shortcutAria}
      aria-pressed={isExpanded}
      title={`AI Insights (${shortcutKeys.join("+")})`}
      className={cn(
        "group border-border hover:bg-accent hover:text-accent-foreground inline-flex shrink-0 items-center gap-1.5 rounded-md border px-2.5 py-1 text-sm transition-colors",
        isExpanded && "bg-accent text-accent-foreground",
        INSIGHTS_AI_RAINBOW_BORDER_CLASS,
        className,
      )}
    >
      <Wand2
        className={cn("size-3.5", spinning && "insights-trigger-spinning")}
      />
      <span className="font-medium">AI Insights</span>
      {/* Hover-revealed shortcut hint. The outer wrapper animates between
          0fr and 1fr grid columns so the contents transition cleanly from
          width 0 to their natural width without us hardcoding a pixel value.
          The inner div needs `overflow-hidden` so the kbd row isn't visible
          while the column is collapsed. */}
      <span
        aria-hidden="true"
        className={cn(
          "ml-0 grid grid-cols-[0fr] opacity-0 transition-[grid-template-columns,opacity,margin-left] duration-200 ease-out",
          "group-hover:ml-1 group-hover:grid-cols-[1fr] group-hover:opacity-100",
          "group-focus-visible:ml-1 group-focus-visible:grid-cols-[1fr] group-focus-visible:opacity-100",
        )}
      >
        <span className="flex items-center gap-1 overflow-hidden">
          {shortcutKeys.map((key) => (
            <kbd
              key={key}
              className="border-border bg-muted text-muted-foreground pointer-events-none inline-flex h-4 min-w-4 items-center justify-center rounded border px-1 font-mono text-[10px] leading-none font-medium select-none"
            >
              {key}
            </kbd>
          ))}
        </span>
      </span>
    </button>
  );
}

/**
 * Page-level config override. Mount this anywhere inside an InsightsProvider
 * to swap in a custom prompt/suggestions/MCP filter. Cleans up on unmount,
 * restoring the provider's defaults.
 */
export function InsightsConfig(options: InsightsConfigOptions) {
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
  suggestions?: Array<{
    title: string;
    label: string;
    prompt: string;
  }>;
  /** Default expanded state. */
  defaultExpanded?: boolean;
  /** Children rendered alongside the sidebar (page content). */
  children: React.ReactNode;
}

const SIDEBAR_MAX_WIDTH = 670;
const SIDEBAR_MAX_PERCENT = 40; // Never more than 40% of viewport

export function InsightsProvider({
  mcpConfig: defaultMcpConfig,
  title: defaultTitle,
  subtitle: defaultSubtitle,
  suggestions: defaultSuggestions = [],
  defaultExpanded = false,
  children,
}: InsightsProviderProps) {
  const [isExpanded, setIsExpanded] = useState(defaultExpanded);
  const [override, setOverride] = useState<InsightsConfigOptions | null>(null);
  // Bumped whenever the keyboard shortcut fires so the trigger plays its
  // one-shot spin animation even when the click didn't originate from the
  // button itself. Starts at 0; consumers ignore the initial value.
  const [triggerSpinKey, setTriggerSpinKey] = useState(0);
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
  const { theme } = useMoonshineConfig();

  // Resolve effective values: per-page override wins, fall back to defaults.
  const mcpConfig = override?.mcpConfig ?? defaultMcpConfig;
  const title = override?.title ?? defaultTitle;
  const subtitle = override?.subtitle ?? defaultSubtitle;
  const suggestions = override?.suggestions ?? defaultSuggestions;
  const contextInfo = override?.contextInfo;
  const hideTrigger = override?.hideTrigger ?? false;
  const noToolsetsConfigured = useNoToolsetsConfigured(mcpConfig.projectSlug);

  // Server-side Project Assistant. Resolved lazily once the sidebar is first
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
            if (messages[i].role === "user") {
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
          const newParts = original.parts.map((p) => {
            if (!prefixed && p.type === "text") {
              prefixed = true;
              return { ...p, text: `${prefix}${p.text}` };
            }
            return p;
          });
          if (!prefixed) {
            newParts.unshift({ type: "text", text: prefix });
          }
          const wrappedMessages: UIMessage[] = [
            ...messages.slice(0, lastUserIdx),
            { ...original, parts: newParts },
            ...messages.slice(lastUserIdx + 1),
          ];
          return inner.sendMessages({ ...args, messages: wrappedMessages });
        },
        reconnectToStream: inner.reconnectToStream.bind(inner),
      };
    };
  }, [serverTransport]);

  const sidebarWidth = `min(${SIDEBAR_MAX_WIDTH}px, ${SIDEBAR_MAX_PERCENT}vw)`;

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
      },
      api: {
        ...mcpConfig.api,
        headers: {
          ...mcpConfig.api?.headers,
          "X-Gram-Source": "dashboard-ai-insights",
        },
      },
      welcome: {
        title,
        subtitle,
        suggestions,
      },
      theme: {
        colorScheme: theme === "dark" ? "dark" : "light",
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

  // Start a brand-new Project Assistant conversation: remount the chat provider
  // (bumping sessionKey) so a fresh thread opens. With server-side id minting,
  // the new thread gets its chat id from the server on the first send.
  const handleStartFresh = useCallback(() => {
    setSessionKey((k) => k + 1);
  }, []);

  // Global keyboard shortcut: Option+Shift+W (Mac) / Alt+Shift+W (PC) toggles
  // the sidebar. Cmd+W is reserved by the browser (closes the tab before JS
  // sees the event), so we deliberately don't bind it here.
  //
  // Skips when the trigger is hidden via `hideTrigger`, when a modifier
  // mismatch is detected (extra Cmd/Ctrl), or when the user is typing in a
  // contentEditable region — letting Alt+Shift+W still work in plain inputs
  // since modifier-heavy combos rarely conflict with text entry.
  useEffect(() => {
    if (hideTrigger) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (!e.altKey || !e.shiftKey) return;
      if (e.metaKey || e.ctrlKey) return;
      // KeyboardEvent.code is layout-independent; e.key on Mac with Option
      // held returns "∑" (the Option+w glyph), which would never match "w".
      if (e.code !== "KeyW") return;
      const target = e.target as HTMLElement | null;
      if (target?.isContentEditable) return;
      e.preventDefault();
      setIsExpanded((prev) => !prev);
      setTriggerSpinKey((k) => k + 1);
    };
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [hideTrigger]);

  const contextValue = useMemo(
    () => ({
      available: !hideTrigger,
      isExpanded,
      setIsExpanded,
      setOverride: handleSetOverride,
      sendPrompt: handleSendPrompt,
      triggerSpinKey,
    }),
    [
      hideTrigger,
      isExpanded,
      handleSetOverride,
      handleSendPrompt,
      triggerSpinKey,
    ],
  );

  return (
    <InsightsContext.Provider value={contextValue}>
      <InsightsRainbowStyles />
      <div className="flex h-full w-full">
        {/* Main content area - shrinks when sidebar opens */}
        <div
          className="min-w-0 flex-1 overflow-hidden transition-all duration-300 ease-in-out"
          style={{
            marginRight: isExpanded ? sidebarWidth : 0,
          }}
        >
          {children}
        </div>

        {/* Backdrop overlay - closes sidebar when clicked */}
        {isExpanded && (
          <div
            className="fixed inset-0 z-20"
            onClick={() => setIsExpanded(false)}
            aria-hidden="true"
          />
        )}

        {/* Sidebar panel - fixed position that slides in.
            Trigger lives in the top breadcrumb bar via <InsightsTrigger />. */}
        <div
          className={cn(
            "bg-background border-border fixed top-0 right-0 bottom-0 z-30 flex flex-col border-l shadow-xl transition-transform duration-300 ease-in-out",
            isExpanded ? "translate-x-0" : "translate-x-full",
          )}
          style={{ width: sidebarWidth }}
        >
          {/* Header */}
          <div className="border-border bg-muted/30 flex items-center justify-between border-b px-4 py-3">
            <div className="flex items-center gap-2">
              <Sparkles className="text-primary size-5" />
              <span className="font-semibold">Project Assistant</span>
            </div>
            <div className="flex items-center gap-1">
              <button
                onClick={handleStartFresh}
                disabled={!assistantReady}
                className="hover:bg-muted rounded p-1.5 transition-colors disabled:cursor-not-allowed disabled:opacity-40"
                aria-label="Start a new conversation"
                title="Start a new conversation"
              >
                <SquarePen className="size-[18px]" />
              </button>
              <button
                onClick={() => setIsExpanded(false)}
                className="hover:bg-muted rounded p-1.5 transition-colors"
                aria-label="Close Project Assistant"
              >
                <ChevronRight className="size-5" />
              </button>
            </div>
          </div>

          {/* Notice when the Project Assistant failed to connect */}
          {assistantError && (
            <div className="border-destructive/40 bg-destructive/10 text-destructive mx-4 mt-3 flex items-start gap-2 rounded-md border px-3 py-2 text-xs">
              <Terminal className="mt-0.5 size-3.5 shrink-0" />
              <span>{assistantError}</span>
            </div>
          )}

          {/* Notice when no toolsets are configured */}
          {noToolsetsConfigured && (
            <div className="border-border bg-muted/50 text-muted-foreground mx-4 mt-3 flex items-start gap-2 rounded-md border px-3 py-2 text-xs">
              <Terminal className="mt-0.5 size-3.5 shrink-0" />
              <span>
                AI tools are unavailable. Create an MCP server to enable the
                Project Assistant.
              </span>
            </div>
          )}

          {/* Chat content — gated on the server assistant being ready so we
              never fall back to client-side generation while connecting. */}
          <div className="flex-1 overflow-hidden">
            {assistantReady ? (
              <GramElementsProvider key={sessionKey} config={elementsConfig}>
                <PendingPromptBridge
                  pending={pendingPrompt}
                  onConsume={consumePendingPrompt}
                />
                <Chat />
              </GramElementsProvider>
            ) : (
              !assistantError && (
                <div className="text-muted-foreground flex h-full items-center justify-center gap-2 text-sm">
                  <Loader2 className="size-4 animate-spin" />
                  <span>Connecting to the Project Assistant…</span>
                </div>
              )
            )}
          </div>
        </div>
      </div>
    </InsightsContext.Provider>
  );
}

/**
 * @deprecated Use <InsightsProvider> at the app shell level + <InsightsConfig>
 * on individual pages that need custom prompts. This alias is kept temporarily
 * to avoid breaking out-of-tree consumers; remove after migration.
 */
export const InsightsSidebar = InsightsProvider;

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
    // The fresh runtime (mounted by sessionKey bump in handleSendPrompt)
    // already starts on a new thread, so just append.
    try {
      assistantRuntime.thread.append(text);
    } catch (err) {
      console.error("Failed to send Explore prompt:", err);
    }

    onConsume();
  }, [pending, assistantRuntime, onConsume]);

  return null;
}
