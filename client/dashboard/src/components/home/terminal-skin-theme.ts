import { createContext } from "react";

/**
 * The Project Assistant runs one shared Elements runtime (InsightsProvider),
 * so its config is global. The homepage terminal opts into the terminal skin
 * by wrapping its <Chat /> in this context instead of swapping the config —
 * a config swap would reset the runtime, and the dock and /chat keep the
 * stock rendering because they never set it. Context crosses the Elements
 * shadow-root portal.
 */
export const TerminalSkinContext = createContext(false);

/** Class on the element that wraps the terminal's <Chat />; scopes the CSS. */
export const TERMINAL_HOST_CLASS = "gram-home-terminal";

/** Collapsed tool results show this many lines, like Claude Code. */
export const MAX_RESULT_LINES = 3;

const host = `:host-context(.${TERMINAL_HOST_CLASS})`;

/**
 * Terminal look for the shared Elements runtime, scoped by :host-context so
 * the dock and /chat are untouched. Colors come from the Elements theme, so
 * the terminal is light in light mode and dark in dark mode; only the
 * status glyph colors are added.
 */
export const TERMINAL_SKIN_CSS = `
  ${host} .gram-elements {
    --term-accent: oklch(0.64 0.15 40);
    --term-ok: oklch(0.62 0.15 150);
    --term-error: oklch(0.6 0.2 25);
    background: var(--background);
    color: var(--foreground);
    font-family: var(--font-mono), ui-monospace, monospace;
    font-size: 13px;
    line-height: 1.6;
  }
  ${host} .gram-elements * {
    font-family: inherit;
  }
  /* One type size, like a terminal; markdown keeps weight, not scale. */
  ${host} .aui-assistant-message-content {
    line-height: 1.6;
  }
  ${host} .aui-assistant-message-content * {
    font-size: inherit;
    line-height: inherit;
  }
  ${host} .aui-thread-scroll-to-bottom {
    border-radius: 0;
    padding: 0.5rem;
    background: var(--card);
    border-color: var(--border);
  }
  /* No thumbs up/down in the terminal. */
  ${host} .aui-feedback-buttons,
  ${host} .aui-feedback-thank-you {
    display: none;
  }

  /* Working state: a cycling glyph and verb in the accent color, in place
     of the rainbow spinner ("✻ Pondering…"). */
  ${host} .gram-term-working {
    display: flex;
    padding: 0.25rem 0 0 0.25rem;
  }
  ${host} .aui-thinking {
    color: var(--term-accent);
    font-size: inherit;
    gap: 0.5rem;
  }
  ${host} .aui-thinking > :not(.aui-thinking-label):not(.sr-only) {
    display: none;
  }
  ${host} .aui-thinking::before {
    content: "✻";
    display: inline-block;
    width: 1ch;
    animation: gram-term-glyph 1.2s steps(1) infinite;
  }
  /* Streaming text ends in a blinking block cursor, not the rainbow ring. */
  ${host} .aui-md[data-status="running"] *::after {
    width: 0.55em;
    height: 1.1em;
    padding: 0;
    margin-left: 0.15rem;
    border-radius: 0;
    background: var(--foreground);
    -webkit-mask: none;
    mask: none;
    animation: gram-term-blink 1s steps(2) infinite;
  }
  ${host} .aui-thread-viewport {
    padding: 1rem 1.25rem 0;
  }

  /* > user prompt line */
  ${host} .aui-user-message-root {
    display: block;
    padding: 0.75rem 0 0.25rem;
  }
  ${host} .aui-user-message-owner {
    display: none;
  }
  ${host} .aui-user-message-content {
    margin: 0;
    width: auto;
    border: 0;
    background: var(--muted);
    color: var(--muted-foreground);
    padding: 0.25rem 0.75rem 0.25rem 2rem;
    position: relative;
  }
  ${host} .aui-user-message-content::before {
    content: ">";
    position: absolute;
    left: 0.75rem;
  }

  /* ⏺ assistant text */
  ${host} .aui-assistant-message-root {
    padding: 0.25rem 0;
  }
  ${host} .aui-assistant-message-content {
    position: relative;
    padding-left: 1.5rem;
  }
  ${host} .aui-assistant-message-content > :not(.gram-term-tools):first-child::before {
    content: "⏺";
    position: absolute;
    left: 0.25rem;
    color: var(--foreground);
  }

  /* Tool lines */
  ${host} .gram-term-tools {
    margin: 0.25rem 0 0.25rem -1.5rem;
  }
  ${host} .gram-term-tool {
    padding: 0.25rem 0;
  }
  ${host} .gram-term-tool-call {
    display: -webkit-box;
    -webkit-line-clamp: 1;
    -webkit-box-orient: vertical;
    overflow: hidden;
    padding-left: 1.5rem;
    text-indent: -1.25rem;
    word-break: break-all;
  }
  ${host} .gram-term-bullet {
    margin-right: 0.5rem;
  }
  ${host} .gram-term-tool .gram-term-bullet {
    color: var(--term-ok);
  }
  ${host} .gram-term-tool[data-state="running"] .gram-term-bullet {
    color: var(--muted-foreground);
    animation: gram-term-blink 1s steps(2) infinite;
  }
  ${host} .gram-term-tool[data-state="error"] .gram-term-bullet {
    color: var(--term-error);
  }
  ${host} .gram-term-tool-name {
    font-weight: 600;
  }
  ${host} .gram-term-tool-args {
    color: var(--muted-foreground);
  }
  ${host} .gram-term-tool-result {
    display: flex;
    gap: 0.75rem;
    padding-left: 1.25rem;
    color: var(--muted-foreground);
  }
  ${host} .gram-term-tool-result pre {
    margin: 0;
    white-space: pre-wrap;
    word-break: break-word;
    font: inherit;
    line-height: 1.5;
  }
  ${host} .gram-term-tool-result pre:not([data-expanded]) {
    display: -webkit-box;
    -webkit-line-clamp: ${MAX_RESULT_LINES};
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  ${host} .gram-term-expand {
    color: var(--muted-foreground);
    font: inherit;
    text-align: left;
  }
  ${host} .gram-term-expand:hover {
    color: var(--foreground);
  }

  /* Welcome box */
  /* Fill the empty thread so the composer sits at the bottom. */
  ${host} .gram-term-welcome {
    flex: 1 0 auto;
    display: flex;
    flex-direction: column;
    gap: 1rem;
    padding-top: 0.5rem;
  }
  ${host} .gram-term-welcome-box {
    border: 1px solid var(--term-accent);
    padding: 0.75rem 1rem;
  }
  ${host} .gram-term-accent {
    color: var(--term-accent);
  }
  ${host} .gram-term-dim {
    color: var(--muted-foreground);
  }
  ${host} .gram-term-tips ol {
    list-style: decimal;
    padding-left: 2rem;
  }
  ${host} .gram-term-tips button:hover {
    color: var(--term-accent);
  }

  /* Boxed composer with a > prompt */
  ${host} .aui-composer-wrapper {
    background: var(--background);
    /* Inside the viewport, which already carries the side padding. */
    padding: 0.5rem 0 1rem;
  }
  ${host} .aui-composer-root {
    border: 1px solid var(--border);
    background: transparent;
    box-shadow: none;
    position: relative;
    padding-left: 1.25rem;
  }
  ${host} .aui-composer-send,
  ${host} .aui-composer-cancel {
    background: transparent;
    color: var(--muted-foreground);
    box-shadow: none;
  }
  ${host} .aui-composer-root:focus-within {
    border-color: var(--muted-foreground);
  }
  ${host} .aui-composer-root::before {
    content: ">";
    position: absolute;
    left: 0.75rem;
    top: 0.6rem;
    color: var(--foreground);
  }

  @keyframes gram-term-blink {
    to { opacity: 0.2; }
  }
  @keyframes gram-term-glyph {
    0% { content: "·"; }
    17% { content: "✢"; }
    33% { content: "✳"; }
    50% { content: "✶"; }
    67% { content: "✻"; }
    83% { content: "✽"; }
  }
  @media (prefers-reduced-motion: reduce) {
    ${host} .gram-term-tool[data-state="running"] .gram-term-bullet,
    ${host} .aui-thinking::before,
    ${host} .aui-md[data-status="running"] *::after {
      animation: none;
    }
  }
`;
