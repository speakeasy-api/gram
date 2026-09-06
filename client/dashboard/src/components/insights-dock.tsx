import { useNoToolsetsConfigured } from "@/hooks/useObservabilityMcpConfig";
import { showProjectAssistantConnecting } from "@/hooks/projectAssistantAccess";
import { useServerAssistantTransport } from "@/hooks/useServerAssistantTransport";
import { useDrainInfiniteQuery } from "@/hooks/useDrainInfiniteQuery";
import { useListChats } from "@gram/client/react-query/listChats.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useSkillsInfinite } from "@gram/client/react-query/skills.js";
import { SortBy, SortOrder } from "@gram/client/models/operations/listchats";
import { cn, isMacPlatform } from "@/lib/utils";
import speakeasyIcon from "@/assets/speakeasy-icon.svg";
import { useAui, useAuiState } from "@assistant-ui/react";
import type { ElementsConfig, ElementsTransportFactory } from "@/elements";
import {
  ActiveChatTitle,
  Chat,
  ChatComposer,
  ChatHistory,
  focusChatComposer,
  GramElementsProvider,
  useThreadId,
} from "@/elements";
import { stripMessageContextFraming } from "@/lib/projectAssistantTranscript";
import { AssistantMarkdownLink } from "@/components/AssistantMarkdownLink";
import { useAssistantLinkResolver } from "@/lib/assistantEntityLinks";
import { useSession } from "@/contexts/Auth";
import { emailsMatch, resolveChatOwner } from "@/lib/chat-owner";
import {
  INSIGHTS_DOCK_CONTENT_VT_CLASS,
  INSIGHTS_DOCK_VT_CLASS,
  useInsightsDockCta,
} from "@/hooks/useInsightsDockCta";
import { useLocation } from "react-router";
import { useRoutes } from "@/routes";
import { motion } from "motion/react";
import {
  getRouteSuggestions,
  INSIGHTS_SUGGESTION_ICONS,
  SLASH_COMMANDS,
  type InsightsSuggestion,
} from "@/lib/insights-suggestions";
import { useConfig as useMoonshineConfig } from "@/components/ui/hooks/useConfig";
import type { UIMessage } from "ai";
import {
  ArrowLeft,
  ArrowUp,
  HistoryIcon,
  Loader2,
  Maximize2,
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
import { InsightsShortcutKeys } from "./insights-dock-shortcut-hint";
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

// How recent a conversation must be (by last message) for the docked pill to
// offer "Continue chat" — reopen it — instead of "Ask anything".
const CONTINUE_WINDOW_MS = 60 * 1000;

/** How recently a grace-timer collapse must have fired for a navigation to
 *  undo it. Covers slow clicks and route transitions that outlast the grace
 *  period (lazy chunks, loaders) — the timer collapses the dock mid-flight
 *  and the location change lands later. Escape and submit collapse without
 *  stamping, so they are never undone. */
const DOCK_REOPEN_WINDOW_MS = 2500;

/** Icon-only buttons in the chat panel's Granola-style header. */
const PANEL_ICON_BUTTON_CLASS =
  "hover:bg-muted text-muted-foreground hover:text-foreground p-1.5 transition-colors";

/**
 * Restyles Elements' default chat composer (tall multi-row box with an
 * attachment/mention toolbar) into the same slim single-line row
 * as the docked pill that opens the panel, so the two read as one control.
 * Injected into the Elements shadow root via `theme.customCss` — host-page
 * CSS can't reach it. Targets Elements' stable `aui-*` class hooks; the
 * toolbar is hidden but typing `@` still triggers tool mentions.
 */
const DOCK_PANEL_COMPOSER_CSS = `
  .aui-composer-wrapper { padding-block: 0.5rem; }
  .aui-composer-root { min-height: 0; border-radius: 0; padding: 0; }
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
  .aui-composer-action-wrapper-inner {
    display: flex;
  }
  /* The pill shows no composition tools at all — attachments, tool mentions
     and skill context all belong to the surfaces with room for them. */
  .aui-composer-action-wrapper-inner > * {
    display: none;
  }
  .aui-composer-skill-context-badges { padding: 0.5rem 3rem 0 0.75rem; }
  /* Mic sits in the same cluster as send, so it has to shrink with it —
     Elements sizes both at 34px, which is right for the full page only. */
  .aui-composer-send, .aui-composer-cancel, .aui-composer-dictate {
    width: 1.75rem;
    height: 1.75rem;
    min-width: 1.75rem;
    cursor: pointer;
  }
  .aui-composer-send-icon, .aui-composer-dictate-icon {
    width: 1rem;
    height: 1rem;
    stroke-width: 1.75;
  }
  .aui-composer-action-send-group { gap: 0.375rem; }
  .aui-composer-dictation-wave { height: 1.75rem; margin-right: 0.375rem; }
  /* Filled square reads heavier than the stroked arrow — smaller icon keeps
     visible padding inside the same-size circle. */
  .aui-composer-cancel-icon {
    width: 0.625rem;
    height: 0.625rem;
  }
`;

// The docked pill already IS the input surface: it draws the border, the card
// background and the shadow. The shared composer rendered inside it must
// therefore contribute no chrome of its own, or the two nest into a
// box-in-a-box. Scoped to the dock host so the panel and page composers keep
// their own frame.
const DOCK_INLINE_COMPOSER_CSS = `
  :host-context(.gram-chat-dock) .aui-composer-wrapper {
    position: static;
    padding: 0;
    background: transparent;
  }
  :host-context(.gram-chat-dock) .aui-composer-root {
    min-height: 0;
    border: 0;
    border-radius: 0;
    padding: 0;
    background: transparent;
    box-shadow: none;
  }
  :host-context(.gram-chat-dock) .aui-composer-input {
    padding: 0.375rem 5.5rem 0.375rem 0;
    min-height: 0;
    margin-bottom: 0;
  }
  :host-context(.gram-chat-dock) .aui-composer-action-wrapper {
    position: absolute;
    right: 0;
    bottom: 50%;
    transform: translateY(50%);
    margin: 0;
  }
  /* The pill has one line: the composer's own "Listening…" label sits where
     the draft text would be. */
  /* The pill is one line wide: keep only the newest half of the level trail
     (the bars nearest the mic) so it can't crowd out the draft row. */
  :host-context(.gram-chat-dock) .aui-composer-dictation-bar:nth-child(-n + 16) {
    display: none;
  }
  :host-context(.gram-chat-dock) .aui-composer-listening {
    padding: 0;
    top: 50%;
    transform: translateY(-50%);
    font-size: 0.875rem;
  }
`;

// Elements' MarkdownText ships display-sized typography (h1 `text-4xl`, h2
// `text-3xl`, 1rem body) tuned for a marketing-style standalone chat. That
// reads oversized in the dashboard's chat surfaces, so dial the body and
// headings down to app scale. Injected after Elements' built-in stylesheet,
// so these equal-specificity `aui-md-*` rules win.
const CHAT_MARKDOWN_CSS = `
  .aui-md { font-size: 0.9375rem; line-height: 1.6; }
  .aui-md-h1 { font-size: 1.5rem; margin-bottom: 1rem; }
  .aui-md-h2 { font-size: 1.3125rem; margin-top: 1.5rem; margin-bottom: 0.75rem; }
  .aui-md-h3 { font-size: 1.125rem; margin-top: 1.25rem; margin-bottom: 0.5rem; }
  .aui-md-h4 { font-size: 1rem; }
  .aui-md-p { margin-top: 0.75rem; margin-bottom: 0.75rem; line-height: 1.6; }
  .aui-md-ul, .aui-md-ol { margin-top: 0.75rem; margin-bottom: 0.75rem; }
`;

// The docked composer above is intentionally compact. The surfaces that own
// the page — the full-page chat (`.gram-chat-fullpage`) and the /chat landing
// (`.gram-chat-landing`) — give the same composer room to breathe: taller
// input, real padding, and the toolbar the dock hides. Scoped via
// :host-context so the docked pill keeps its tight single-line form.
const roomyComposerCss = (scope: string) => `
  :host-context(.${scope}) .aui-composer-root {
    min-height: 6.5rem;
    padding: 0.75rem 0.375rem 0;
  }
  :host-context(.${scope}) .aui-composer-input {
    min-height: 2.75rem;
    padding: 0 1.125rem 0.5rem;
    font-size: 1rem;
    line-height: 1.5rem;
  }
  :host-context(.${scope}) .aui-composer-action-wrapper {
    position: static;
    margin: 0 0.5rem 0.625rem;
    transform: none;
  }
  :host-context(.${scope}) .aui-composer-action-wrapper-inner > * {
    display: inline-flex;
  }
  :host-context(.${scope}) .aui-composer-skill-context-picker-label {
    display: inline;
  }
  :host-context(.${scope}) .aui-composer-send,
  :host-context(.${scope}) .aui-composer-cancel,
  :host-context(.${scope}) .aui-composer-dictate {
    width: 2.125rem;
    height: 2.125rem;
    min-width: 2.125rem;
  }
  :host-context(.${scope}) .aui-composer-send-icon,
  :host-context(.${scope}) .aui-composer-dictate-icon {
    width: 1.125rem;
    height: 1.125rem;
  }
  :host-context(.${scope}) .aui-composer-dictation-wave { height: 2.125rem; }
`;

const CHAT_FULLPAGE_COMPOSER_CSS = `
  ${roomyComposerCss("gram-chat-fullpage")}
  :host-context(.gram-chat-fullpage) .aui-composer-wrapper {
    padding-bottom: 1.5rem;
  }
`;

// The /chat landing renders the same composer standalone (no thread), so it
// also has to undo the wrapper's sticky-bottom band, which has nothing to
// stick to here.
const CHAT_LANDING_COMPOSER_CSS = `
  ${roomyComposerCss("gram-chat-landing")}
  :host-context(.gram-chat-landing) .aui-composer-wrapper {
    position: static;
    padding: 0;
  }
  /* Cycling example prompts. The text arrives as a custom property set on the
     host element — custom properties inherit through the shadow boundary, so
     the landing can crossfade through examples without re-rendering the chat
     tree (a changing \`composer.placeholder\` in the Elements config would).
     The composer's own placeholder is hidden because it cannot be
     transitioned — it is a ::before on the contenteditable input, since a div
     has no ::placeholder. */
  :host-context(.gram-chat-landing) .aui-composer-input[data-empty="true"]::before {
    content: none;
  }
  :host-context(.gram-chat-landing) .aui-composer-root[data-empty="true"]::before {
    content: var(--gram-composer-placeholder, "Ask anything");
    position: absolute;
    /* Must land exactly where the caret does: absolute offsets are measured
       from the root's padding box, so this is the root's own padding plus the
       input's — 0.375rem + 1.125rem across, 0.75rem down — with the input's
       line-height so the first line sits on the same baseline. */
    left: 1.5rem;
    top: 0.75rem;
    font-size: 1rem;
    line-height: 1.5rem;
    color: var(--muted-foreground);
    opacity: var(--gram-composer-placeholder-opacity, 1);
    transition: opacity 300ms ease;
    pointer-events: none;
  }
  /* Focus means the user is about to type — the rotating example would sit
     under their caret, so it clears out of the way. */
  :host-context(.gram-chat-landing) .aui-composer-root:focus-within::before {
    content: none;
  }
  /* The composer paints its own "Listening…" while dictating — don't stack the
     cycling example prompt behind it. */
  :host-context(.gram-chat-landing) .aui-composer-root[data-dictating="true"]::before {
    content: none;
  }
`;

// Assistant replies render links with Moonshine's <Link> (class
// `text-link-primary`), but Moonshine's stylesheet — which carries the rule
// `.text-link-primary { color: var(--text-link-primary) }` — isn't loaded in
// the Elements shadow root, so the class has no effect and the link inherits
// the body text color. Color the link directly with the Moonshine brand blue
// (light/dark); the inner text span inherits it.
const CHAT_LINK_CSS = `
  .gram-elements a.text-link-primary { color: #1e5aae; }
  .gram-elements.dark a.text-link-primary,
  .dark .gram-elements a.text-link-primary { color: #619aea; }
`;

function DockSubmitButton() {
  return (
    <button
      type="submit"
      aria-label="Send to Project Assistant"
      className="bg-primary text-primary-foreground hover:bg-primary/90 flex size-6 shrink-0 items-center justify-center transition-colors"
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
  /** Submit a fresh conversation (the resting "Ask anything" state). */
  onSubmitPrompt: (text: string) => void;
  /** Reopen the most recent conversation (the "Continue chat" button). */
  onContinue: () => void;
  /** When true the resting pill is a "Continue chat" button that reopens the
   *  recent conversation (a chat was active in the last few minutes). */
  continueMode: boolean;
  /** Genies the dock down into the sidebar-footer resume button. */
  onDismiss: () => void;
  /** Opens the chat panel straight into the full-window history view. */
  onOpenHistory: () => void;
  /** Chat panel content, rendered inside the card when `open`. */
  panel: React.ReactNode;
  /** True once the shared Elements runtime is mounted. The pill then renders
   *  the same AUI composer every other surface uses (dictation, attachments,
   *  tool mentions); until then it falls back to a plain input so the dock is
   *  never a dead control. */
  runtimeReady: boolean;
}

/** Width of the dock card across its states: chat panel open, composer
 *  focused (or holding draft text), and collapsed bar. The card itself
 *  carries the single floating-overlay shadow. */
function dockCardShapeClass(open: boolean, composerExpanded: boolean): string {
  if (open) return "max-w-3xl";
  if (composerExpanded) return "max-w-2xl";
  return "max-w-md";
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
  onContinue,
  continueMode,
  onDismiss,
  onOpenHistory,
  panel,
  runtimeReady,
}: InsightsDockProps): ReactElement {
  const [value, setValue] = useState("");
  // Expansion is sticky state, not a focus mirror: it must survive the input
  // losing focus to an in-app navigation so the new page's suggestions show
  // without re-opening the dock. Collapses commit through a grace timer that
  // a route change can cancel (or, for slow clicks, undo).
  const [expanded, setExpanded] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  /** Wraps the shared composer; also the focus target for Cmd+/. */
  const composerHostRef = useRef<HTMLDivElement>(null);
  /** Whether the shared composer holds a draft (see ComposerDraftReporter). */
  const [sharedDraft, setSharedDraft] = useState(false);
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
  // The AUI composer's input lives in a shadow root, so it can't be reached
  // with a plain ref.
  useEffect(() => {
    if (focusKey === 0) return;
    if (runtimeReady) {
      focusChatComposer(composerHostRef.current);
      return;
    }
    inputRef.current?.focus();
  }, [focusKey, runtimeReady]);

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

  // `value` only backs the fallback input; the shared composer keeps its draft
  // on the runtime, so it reports emptiness up through `sharedDraft`. Both feed
  // the same rule: a non-empty draft holds the dock open even without focus.
  const hasDraft = value.trim().length > 0 || sharedDraft;
  const composerExpanded = !open && (expanded || hasDraft);

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

  // Within the continue window the resting pill is a "Continue chat" button
  // that reopens the recent conversation instead of an "Ask anything" input.
  const composerField = continueMode ? (
    <button
      type="button"
      onClick={onContinue}
      tabIndex={open ? -1 : 0}
      className="text-muted-foreground hover:text-foreground min-w-0 flex-1 cursor-pointer text-left text-sm transition-colors"
    >
      Continue chat
    </button>
  ) : runtimeReady ? (
    // The shared composer, sending straight through the runtime — no prompt
    // hand-off, so the dock gets dictation and every other composer feature.
    <div
      ref={composerHostRef}
      className="gram-chat-dock min-w-0 flex-1"
      tabIndex={open ? -1 : undefined}
    >
      <ComposerDraftReporter onChange={setSharedDraft} />
      <ChatComposer />
    </div>
  ) : (
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
  );

  return (
    <div
      className={cn(
        "pointer-events-none absolute inset-x-0 bottom-0 z-30 flex justify-center px-4 pt-14 pb-12",
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
          "border-border bg-card text-card-foreground pointer-events-auto w-full border shadow-md",
          "transition-all duration-300 ease-out",
          dockCardShapeClass(open, composerExpanded),
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
          // Focus inside the shadow-rooted composer retargets to its host, so
          // match on containment rather than identity for that path.
          const fromComposer =
            e.target === inputRef.current ||
            (composerHostRef.current?.contains(e.target as Node) ?? false);
          if (fromComposer) {
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
        <div className={INSIGHTS_DOCK_CONTENT_VT_CLASS}>
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
            )}
          >
            <div className="overflow-hidden">
              {/* Same grey-tray-around-white-surface treatment as the
                  expanded composer, so opening the chat reads as the input
                  surface growing into the conversation rather than an
                  unrelated white panel appearing. */}
              <div className="bg-muted/40 p-2">
                <div className="border-border bg-card h-[min(640px,70vh)] overflow-hidden border">
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
            {/* The clip is what makes the grid-rows collapse animate, but it
                also crops the composer's own menus (slash commands, tool
                mentions), which open upwards out of the composer box. Elements
                marks its shadow host while one is open, so the clip lifts for
                exactly as long as there is a menu to show. */}
            <div className="overflow-hidden has-[[data-composer-menu-open]]:overflow-visible">
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
                  "transition-[padding,background-color] duration-300 ease-out",
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
                                className="border-border bg-card text-muted-foreground hover:bg-accent hover:text-accent-foreground flex items-center gap-1.5 border px-2 py-1 text-xs transition-colors"
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
                    "flex items-center gap-2.5 border px-4 py-2.5 transition-colors duration-300 ease-out",
                    composerExpanded
                      ? "border-border bg-card"
                      : "border-transparent bg-transparent",
                  )}
                >
                  {composerField}
                  {value.trim() && <DockSubmitButton />}
                  {composerExpanded && (
                    <button
                      type="button"
                      onClick={onOpenHistory}
                      className={PANEL_ICON_BUTTON_CLASS}
                      aria-label="Conversation history"
                      title="History"
                    >
                      <HistoryIcon className="size-3.5" />
                    </button>
                  )}
                  {/* Greyed shortcut hint on the resting pill; hidden once the
                      composer is engaged (focused or typing), in continue mode
                      (a button, not a focusable input), and alongside the
                      shared composer — whose own action cluster (mic, send,
                      mentions) already crowds that corner. */}
                  {!composerExpanded && !continueMode && !runtimeReady && (
                    <InsightsShortcutKeys className="opacity-60" />
                  )}
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
  // Ref-counted dock hide for pages with their own chat entry (full-page chat,
  // home widget). Kept separate from `override` so dashboard consumers that
  // reset the override (ProjectDashboard) can't un-hide the dock.
  const dockHideCountRef = useRef(0);
  const [dockHiddenByPage, setDockHiddenByPage] = useState(false);
  const registerDockHide = useCallback(() => {
    dockHideCountRef.current += 1;
    setDockHiddenByPage(dockHideCountRef.current > 0);
    return () => {
      dockHideCountRef.current = Math.max(0, dockHideCountRef.current - 1);
      setDockHiddenByPage(dockHideCountRef.current > 0);
    };
  }, []);
  // Bumped whenever the keyboard shortcut fires (with the chat panel closed) so
  // the docked composer grabs focus and expands. Starts at 0; the dock
  // ignores the initial value.
  const [focusComposerKey, setFocusComposerKey] = useState(0);
  const [pendingPrompt, setPendingPrompt] = useState<{
    text: string;
    nonce: number;
  } | null>(null);
  // Turns the assistant's `gram:<entity>` references into clickable links into
  // the dashboard, opened in a new tab. Rendered via Moonshine's <Link>.
  const resolveAssistantLink = useAssistantLinkResolver();
  // React `key` on the shared GramElementsProvider — bumped to start a
  // brand-new conversation ("New", "Explore with AI", a fresh docked send).
  // switchToNewThread on the long-lived shared runtime trips an assistant-ui
  // projection race (tapLookupResources: __LOCALID_… not found), so we remount
  // the runtime onto a fresh thread instead. The runtime is shared across the
  // dock and the full-page chat, but the key only bumps when the user starts a
  // new conversation — never when navigating between the two — so an in-flight
  // turn survives maximize.
  const [runtimeKey, setRuntimeKey] = useState(0);
  // History takes over the whole panel (replacing the chat) rather than
  // dropping a popover, and is cancellable back to the chat. Reset on close
  // and on a fresh conversation so reopening always lands on the chat.
  const [historyView, setHistoryView] = useState(false);
  // Whether the history view has a live conversation to return to. False when
  // opened cold from the docked composer (no chat exists behind it yet), so
  // the back affordance is hidden and only close is offered. True when opened
  // from inside an active chat via the in-panel history button.
  const [historyReturnable, setHistoryReturnable] = useState(false);
  useEffect(() => {
    if (!isExpanded) {
      setHistoryView(false);
      setHistoryReturnable(false);
    }
  }, [isExpanded]);
  const { theme } = useMoonshineConfig();
  const { pathname } = useLocation();
  const routes = useRoutes();
  // The full-page chat lives at `/…/chat[/…]`. The shared assistant runtime is
  // resolved when the dock is opened OR when on a chat route, so the page has a
  // live runtime without the user touching the dock first.
  const onChatRoute = /\/chat(\/|$)/.test(pathname);
  // On a chat route the page owns the chat and the dock is hidden, so collapse
  // the dock (a maximize leaves it expanded). The shared runtime stays mounted
  // via onChatRoute, so this collapse never unmounts it.
  useEffect(() => {
    if (onChatRoute) setIsExpanded(false);
  }, [onChatRoute]);

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
  const hideTrigger = (override?.hideTrigger ?? false) || dockHiddenByPage;
  const noToolsetsConfigured = useNoToolsetsConfigured(mcpConfig.projectSlug);
  const [selectedSkillIds, setSelectedSkillIds] = useState<string[]>([]);
  const selectedSkillIdsRef = useRef(selectedSkillIds);
  selectedSkillIdsRef.current = selectedSkillIds;
  const getSelectedSkillIds = useCallback(
    () => selectedSkillIdsRef.current,
    [],
  );
  const handleSkillIdsSent = useCallback((sentSkillIds: string[]) => {
    const sent = new Set(sentSkillIds);
    setSelectedSkillIds((current) =>
      current.filter((skillID) => !sent.has(skillID)),
    );
  }, []);

  useEffect(() => {
    setSelectedSkillIds([]);
  }, [mcpConfig.projectSlug]);

  // Server-side Project Assistant. Resolved lazily the first time the chat
  // panel is opened or a chat route is visited; once resolved it stays, so the
  // shared runtime (below) persists across navigation. While connecting (or
  // after a failure) the factory is undefined and we gate the chat instead of
  // falling back to client-side generation. assistantId scopes the
  // conversation list to this assistant's chats.
  const {
    transport: serverTransport,
    assistantId: managedAssistantId,
    ready: assistantReady,
    error: assistantError,
    needsAdmin: assistantNeedsAdmin,
  } = useServerAssistantTransport(mcpConfig.projectSlug, true, {
    getSkillIds: getSelectedSkillIds,
    onSkillIdsSent: handleSkillIdsSent,
  });

  const skillsQuery = useSkillsInfinite(
    { limit: 200, gramProject: mcpConfig.projectSlug },
    undefined,
    {
      enabled: assistantReady,
      throwOnError: false,
    },
  );
  useDrainInfiniteQuery(skillsQuery, assistantReady);
  const composerSkills = useMemo(
    () =>
      (skillsQuery.data?.pages.flatMap((page) => page.result.skills) ?? [])
        .filter((skill) => skill.hasValidVersion)
        .map((skill) => ({
          id: skill.id,
          name: skill.name,
          displayName: skill.displayName,
          summary: skill.summary,
        })),
    [skillsQuery.data?.pages],
  );

  // Derive "Continue chat" from the server: if the viewer's most recent
  // dashboard-initiated session with the assistant was active within
  // CONTINUE_WINDOW_MS, the resting pill offers to reopen it. Reading the
  // backend (rather than client state) means it survives reloads for free.
  // sourceKind and userId keep out other members' sessions and other sources
  // (Slack, cron, …), which an admin's chat:read visibility would otherwise
  // surface here. limit:1 — we only need the newest.
  const { user } = useSession();
  const { data: recentChatsData } = useListChats(
    {
      assistantId: managedAssistantId || undefined,
      sourceKind: "dashboard",
      userId: user.id,
      sortBy: SortBy.LastMessageTimestamp,
      sortOrder: SortOrder.Desc,
      limit: 1,
    },
    undefined,
    { enabled: Boolean(managedAssistantId), throwOnError: false },
  );
  const recentChat = recentChatsData?.chats?.[0];
  const continueMode =
    recentChat !== undefined &&
    Date.now() - recentChat.lastMessageTimestamp.getTime() < CONTINUE_WINDOW_MS;

  // Resolves a chat's creator to a name/email/avatar for Elements' history
  // list, from the org member list the dashboard already has cached — no
  // extra request, and avoids the cross-origin auth mismatch a direct fetch
  // from inside Elements would hit (its request headers are scoped to the
  // chat API, not `access.listMembers`).
  const { data: membersData } = useMembers();
  const resolveCreator = useCallback(
    ({
      userId,
      externalUserId,
    }: {
      userId?: string;
      externalUserId?: string;
    }) => {
      if (!userId && !externalUserId) return undefined;
      // Chats started from the dashboard itself have no `userId` at capture
      // time and stash the caller's email in `externalUserId` instead —
      // resolveChatOwner falls back to a case-insensitive email match so
      // those still resolve to a member.
      const member = resolveChatOwner(membersData?.members, {
        userId,
        externalUserId,
      });
      return (
        member && {
          name: member.name,
          email: member.email,
          photoUrl: member.photoUrl,
        }
      );
    },
    [membersData],
  );

  // The backend only lets a chat's creator send into it (see
  // CheckDashboardChatOwnership) — admins can still open others' chats via
  // their chat:read grant, so hide the composer for those rather than let a
  // send 404. Chats started from the dashboard stash the caller's email in
  // externalUserId instead of userId (see resolveCreator above).
  const isOwnChat = useCallback(
    ({
      userId,
      externalUserId,
    }: {
      userId?: string;
      externalUserId?: string;
    }) => {
      if (!userId && !externalUserId) return true;
      return userId === user.id || emailsMatch(externalUserId, user.email);
    },
    [user.id, user.email],
  );

  // The shared composer needs its runtime as soon as the assistant resolves,
  // including on Home where the floating dock is hidden in favor of the
  // landing widget. Routes that mount their own GramElementsProvider are the
  // exception: wrapping them would nest RemoteThreadListRuntimes before their
  // useHideInsightsDock layout effect can register with this parent.
  const pageOwnsRuntime =
    routes.playground.active ||
    routes.assistants.newAssistant.active ||
    routes.assistants.detail.active;
  const runtimeMounted = assistantReady && !pageOwnsRuntime;

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
      // Link entity references in assistant replies to their dashboard pages
      // (new tab). `resolveLink` maps the `gram:` scheme to routes;
      // `linkComponent` renders every link with Moonshine's <Link>.
      resolveLink: resolveAssistantLink,
      linkComponent: AssistantMarkdownLink,
      // Edit relies on assistant-ui's local branch rewriting, which the
      // server-side assistant transport can't honour — hide the affordance
      // rather than ship a control that silently no-ops.
      allowMessageEdit: false,
      // History, the conversation list, and titles come from the chat service
      // via Elements' thread-list adapter, scoped to this assistant's chats. The
      // assistant mints chat ids server-side, so defer client-side id minting.
      // Exclude source_kind=setup so client-driven onboarding threads for this
      // (managed) assistant never surface in the runtime AI Insights history.
      history: {
        enabled: true,
        threadListFilters: {
          assistant_id: managedAssistantId,
          exclude_source_kind: "setup",
        },
        deferThreadIdMinting: true,
        // The runtime persists each turn with a backend `<message-context>`
        // framing block (needed for replay, noise for display). Strip it — and
        // drop framing-only turns — before Elements renders the transcript.
        transformChatMessage: stripMessageContextFraming,
        resolveCreator,
        isOwnChat,
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
      // same control the user just typed into.
      composer: {
        placeholder: "Ask anything",
        // Typing `/` offers the same canned prompts the landing widget used to
        // own, now that every surface shares one composer.
        slashCommands: SLASH_COMMANDS.map((command) => ({
          title: command.title,
          label: command.label,
          prompt: command.prompt,
        })),
        skillContext: {
          skills: composerSkills,
          selectedSkillIds,
          onSelectedSkillIdsChange: setSelectedSkillIds,
          loading: skillsQuery.isPending || skillsQuery.isFetchingNextPage,
          error: !!skillsQuery.error,
          maxSelected: 10,
        },
      },
      theme: {
        colorScheme: theme === "dark" ? "dark" : "light",
        // Square corners throughout the embedded chat, matching the
        // dashboard's flat design language.
        radius: "sharp",
        customCss:
          DOCK_PANEL_COMPOSER_CSS +
          DOCK_INLINE_COMPOSER_CSS +
          CHAT_MARKDOWN_CSS +
          CHAT_FULLPAGE_COMPOSER_CSS +
          CHAT_LANDING_COMPOSER_CSS +
          CHAT_LINK_CSS,
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
      composerSkills,
      selectedSkillIds,
      skillsQuery.isPending,
      skillsQuery.isFetchingNextPage,
      skillsQuery.error,
      resolveAssistantLink,
      resolveCreator,
      isOwnChat,
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

  // "Explore with AI" / docked composer / chat-home composer all route here.
  // Bump the runtime key so a fresh conversation opens, then queue the prompt
  // for the bridge to append once the new runtime mounts. Nonce defeats
  // reference-equality skipping when the same chart is clicked twice in a row.
  const handleSendPrompt = useCallback((text: string) => {
    setRuntimeKey((k) => k + 1);
    setPendingPrompt({ text, nonce: Date.now() });
    // Sending always drops into the live conversation, never the history list.
    setHistoryView(false);
  }, []);

  // Docked-pill "Continue chat": reopen the recent conversation as its own
  // full page. Navigating (rather than reopening the dock panel) loads the
  // exact chat by id via the page's runtime, so it works across reloads too.
  const handleReopenChat = useCallback(() => {
    if (recentChat) routes.chat.conversation.goTo(recentChat.id);
  }, [recentChat, routes]);

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

  // Start a brand-new Project Assistant conversation. Bumping the runtime key
  // remounts the shared runtime onto a fresh thread; the new thread gets its
  // chat id from the server on the first send.
  const handleStartFresh = useCallback(() => {
    setRuntimeKey((k) => k + 1);
    setHistoryView(false);
  }, []);

  // Opening history from the docked composer: open the panel (which lazily
  // resolves the assistant transport the history list needs) straight into
  // the full-window history view. No chat exists behind it yet, so the view
  // is close-only — no back affordance.
  const handleOpenHistory = useCallback(() => {
    setIsExpanded(true);
    setHistoryView(true);
    setHistoryReturnable(false);
  }, []);

  // Expand: leave the floating dock for the full-page chat, opening the
  // current conversation (or a fresh one when none exists yet). Navigate only —
  // do NOT collapse here: keeping isExpanded set means `onChatRoute` takes over
  // as the runtime-mount condition before isExpanded flips, so the shared
  // runtime never unmounts mid-transition and the in-flight stream survives.
  // The chat-route effect below collapses the dock once we've landed.
  const handleExpandToPage = useCallback(
    (threadId: string | null) => {
      routes.chat.conversation.goTo(threadId ?? "new");
    },
    [routes],
  );

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
      // Expose the gated "runtime is mounted" signal, not the raw eager
      // `assistantReady`: chat pages render AUI hooks
      // only once the provider actually exists.
      assistantReady: runtimeMounted,
      assistantNeedsAdmin,
      newConversation: handleStartFresh,
      registerDockHide,
    }),
    [
      hideTrigger,
      isExpanded,
      handleSetOverride,
      handleSendPrompt,
      runtimeMounted,
      assistantNeedsAdmin,
      handleStartFresh,
      registerDockHide,
    ],
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

  // Cancels the full-window history view back to the chat — reads "← Back".
  const panelBackButton = (
    <button
      onClick={() => setHistoryView(false)}
      className={cn(PANEL_ICON_BUTTON_CLASS, "flex items-center gap-1")}
      aria-label="Back to conversation"
      title="Back to conversation"
    >
      <ArrowLeft className="size-4" />
      <span className="text-sm font-medium">Back</span>
    </button>
  );

  const panelNotices = (
    <>
      {/* Notice when the Project Assistant failed to connect */}
      {assistantError && (
        <div className="border-destructive/40 bg-destructive/10 text-destructive mx-4 mt-1 flex items-start gap-2 border px-3 py-2 text-xs">
          <Terminal className="mt-0.5 size-3.5 shrink-0" />
          <span>{assistantError}</span>
        </div>
      )}

      {assistantNeedsAdmin && (
        <div className="border-border bg-muted/50 text-muted-foreground mx-4 mt-1 flex items-start gap-2 border px-3 py-2 text-xs">
          <Terminal className="mt-0.5 size-3.5 shrink-0" />
          <span>
            Ask an admin to enable the Project Assistant for this project.
          </span>
        </div>
      )}

      {/* Notice when no toolsets are configured */}
      {noToolsetsConfigured && (
        <div className="border-border bg-muted/50 text-muted-foreground mx-4 mt-1 flex items-start gap-2 border px-3 py-2 text-xs">
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
  // conversation, full-page chat, and close on the right. The chat runtime is
  // the shared GramElementsProvider mounted around the whole dock + outlet (see
  // the return below), so this panel just renders against it.
  const panelContent = (
    <div className="flex h-full flex-col">
      {runtimeMounted ? (
        <div className="flex h-full min-h-0 flex-col">
          {/* History view: the conversation list takes over the whole panel
                in place of the chat, cancellable via the back button. */}
          {historyView && (
            <>
              <div
                className={cn(
                  "flex shrink-0 items-center px-2 pt-2",
                  historyReturnable ? "justify-between" : "justify-end",
                )}
              >
                {historyReturnable && panelBackButton}
                {panelCloseButton}
              </div>
              {/* Picking a conversation (or "New Thread") switches the
                    runtime's active thread; the list lives in a shadow root,
                    so its click bubbles (composed) out to this wrapper and
                    returns the user to the chat. */}
              <div
                className="min-h-0 flex-1 overflow-y-auto"
                onClick={() => setHistoryView(false)}
              >
                <ChatHistory className="min-h-full" />
              </div>
            </>
          )}
          {/* Chat view */}
          {!historyView && (
            <>
              <div className="flex shrink-0 items-center justify-between gap-1 px-2 pt-2">
                <div className="flex min-w-0 items-center gap-0.5">
                  <button
                    onClick={() => {
                      setHistoryReturnable(true);
                      setHistoryView(true);
                    }}
                    className={PANEL_ICON_BUTTON_CLASS}
                    aria-label="Conversation history"
                    title="History"
                  >
                    <HistoryIcon className="size-4" />
                  </button>
                  <ActiveChatTitle className="min-w-0 flex-1" />
                </div>
                <div className="flex shrink-0 items-center gap-0.5">
                  <button
                    onClick={handleStartFresh}
                    className={PANEL_ICON_BUTTON_CLASS}
                    aria-label="Start a new conversation"
                    title="Start a new conversation"
                  >
                    <SquarePen className="size-4" />
                  </button>
                  <ExpandToPageButton onExpand={handleExpandToPage} />
                  {panelCloseButton}
                </div>
              </div>
              {panelNotices}
              <div className="min-h-0 flex-1 overflow-hidden">
                <Chat />
              </div>
            </>
          )}
        </div>
      ) : (
        <>
          {/* Header (connecting state) — close only; the conversation list
              and new-chat need the chat runtime. The back affordance only
              applies to history opened from a live chat (not a cold composer
              open), so it tracks historyReturnable like the ready header. */}
          <div
            className={cn(
              "flex shrink-0 items-center px-2 pt-2",
              historyView && historyReturnable
                ? "justify-between"
                : "justify-end",
            )}
          >
            {historyView && historyReturnable && panelBackButton}
            {panelCloseButton}
          </div>
          {panelNotices}
          {showProjectAssistantConnecting({
            assistantError,
            assistantNeedsAdmin,
            noMcpAccessConfigured: noToolsetsConfigured,
          }) && (
            <div className="text-muted-foreground flex flex-1 items-center justify-center gap-2 text-sm">
              <Loader2 className="size-4 animate-spin" />
              <span>Connecting to the Project Assistant…</span>
            </div>
          )}
        </>
      )}
    </div>
  );

  // Page content (outlet) + the docked composer. Relative so the composer
  // floats at the bottom-center of the content area.
  const dockSurface = (
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
          onContinue={handleReopenChat}
          continueMode={continueMode}
          onDismiss={handleDockDismiss}
          onOpenHistory={handleOpenHistory}
          panel={panelContent}
          runtimeReady={runtimeMounted}
        />
      )}
    </div>
  );

  return (
    <InsightsContext.Provider value={contextValue}>
      <InsightsRainbowStyles />
      {/* The dock and the full-page chat share ONE runtime so an in-flight
          conversation survives moving between them. The assistant id resolves
          eagerly (for the "Continue chat" lookup), but the runtime only mounts
          where chat is actually shown — the open dock or a chat route — to
          avoid running MCP discovery on every page. It remounts (via
          runtimeKey) only when a new conversation is started; PendingPromptBridge
          appends any queued prompt to the fresh thread. */}
      {runtimeMounted ? (
        <GramElementsProvider
          key={`${mcpConfig.projectSlug}:${runtimeKey}`}
          config={elementsConfig}
        >
          <PendingPromptBridge
            pending={pendingPrompt}
            onConsume={consumePendingPrompt}
          />
          <StopDictationOnNavigate />
          <OpenPanelOnSend
            enabled={!isExpanded && !onChatRoute && !hideTrigger}
            onSend={() => setIsExpanded(true)}
          />
          {dockSurface}
        </GramElementsProvider>
      ) : (
        dockSurface
      )}
    </InsightsContext.Provider>
  );
}

/**
 * Expand affordance in the chat panel header. Reads the current conversation
 * id from the runtime (so the full-page chat opens the same thread) and hands
 * it to the provider, which navigates to `/chat/:chatId`. Lives inside
 * GramElementsProvider so `useThreadId` resolves.
 */
function ExpandToPageButton({
  onExpand,
}: {
  onExpand: (threadId: string | null) => void;
}): ReactElement {
  const { threadId } = useThreadId();
  return (
    <button
      onClick={() => onExpand(threadId)}
      className={PANEL_ICON_BUTTON_CLASS}
      aria-label="Open in full page"
      title="Open in full page"
    >
      <Maximize2 className="size-4" />
    </button>
  );
}

/**
 * Opens the chat panel when the docked composer sends. The dock's composer
 * submits through the runtime itself, so there is no submit handler to hook.
 * Disabled on chat routes and while the panel is already open, where the panel
 * must not steal the view.
 */
function OpenPanelOnSend({
  enabled,
  onSend,
}: {
  enabled: boolean;
  onSend: () => void;
}): null {
  // A run starting, not the message count growing: loading an existing
  // conversation into the shared runtime also grows that count, which would
  // pop the panel open on its own.
  const isRunning = useAuiState(({ thread }) => thread.isRunning);
  const wasRunningRef = useRef(isRunning);
  useEffect(() => {
    const justStarted = isRunning && !wasRunningRef.current;
    wasRunningRef.current = isRunning;
    if (justStarted && enabled) onSend();
  }, [isRunning, enabled, onSend]);
  return null;
}

/**
 * Reports whether the shared composer holds a draft. The docked pill stays
 * expanded while the user has text in it — with the legacy input that was
 * local state, but the shared composer keeps its draft on the runtime, so the
 * dock has to be told. Without this a typed draft is hidden the moment focus
 * leaves the composer.
 */
function ComposerDraftReporter({
  onChange,
}: {
  onChange: (hasDraft: boolean) => void;
}): null {
  const hasDraft = useAuiState(({ composer }) => composer.text.trim() !== "");
  useEffect(() => {
    onChange(hasDraft);
  }, [hasDraft, onChange]);
  // Retract the report when the composer goes away — the dock swaps it out for
  // the "Continue chat" button whenever a recent conversation appears, and a
  // last report of `true` would otherwise pin the dock open with nothing in it.
  useEffect(
    () => () => {
      onChange(false);
    },
    [onChange],
  );
  return null;
}

/**
 * Ends any live dictation when the route changes. The composer is shared and
 * outlives navigation, so without this the mic keeps listening (and recording)
 * on a page the user has already left.
 */
function StopDictationOnNavigate(): null {
  const { pathname } = useLocation();
  const aui = useAui();
  const isDictating = useAuiState(({ composer }) => composer.dictation != null);
  const dictatingRef = useRef(isDictating);
  dictatingRef.current = isDictating;
  useEffect(() => {
    return () => {
      if (dictatingRef.current) aui.composer().stopDictation();
    };
    // Cleanup runs on every pathname change, which is exactly the signal.
  }, [pathname, aui]);
  return null;
}

/**
 * assistant-ui's EMPTY_THREAD_CORE placeholder throws a single shared sentinel
 * Error on append (and other actions) until the real thread core binds. It's
 * not exported, so we match its message to tell "thread not ready yet, retry"
 * apart from a genuine send failure.
 */
function isEmptyThreadError(err: unknown): boolean {
  return err instanceof Error && err.message.includes("empty thread");
}

/**
 * Lives inside the shared GramElementsProvider. When a prompt is queued
 * (sendPrompt / docked send / chat-home composer), the runtime has just been
 * remounted onto a fresh thread via its `key`, so this bridge simply appends
 * the prompt to that thread — no switchToNewThread (which races on the
 * long-lived shared runtime). Fires once per nonce.
 */
function PendingPromptBridge({
  pending,
  onConsume,
}: {
  pending: { text: string; nonce: number } | null;
  onConsume: () => void;
}): null {
  const thread = useAui().thread();
  const isThreadReady = useAuiState(
    ({ thread }) => !thread.isLoading && !thread.isDisabled,
  );
  const firedNonceRef = useRef<number | null>(null);

  useEffect(() => {
    if (!pending || !isThreadReady) return;
    if (firedNonceRef.current === pending.nonce) return;
    firedNonceRef.current = pending.nonce;

    const { text } = pending;

    // On a cold open the main thread is a placeholder (assistant-ui's
    // EMPTY_THREAD_CORE) until the remote thread-list runtime binds the real
    // thread core. The placeholder reports isLoading/isDisabled false but
    // throws on append, so a throw here means nothing was sent — leave the
    // prompt queued and retry on the next runtime event. A successful append
    // marks `done` immediately and never retries: the optimistic message lands
    // asynchronously, so a post-append "did it land?" check races and
    // re-appending duplicates the send.
    let done = false;

    const finish = () => {
      done = true;
      onConsume();
    };

    const attempt = () => {
      if (done) return;
      try {
        thread.append(text);
      } catch (err) {
        if (!isEmptyThreadError(err)) {
          console.error("Failed to send queued assistant prompt:", err);
          finish();
        }
        return;
      }
      finish();
    };

    attempt();

    return () => {
      done = true;
    };
  }, [pending, isThreadReady, thread, onConsume]);

  return null;
}
