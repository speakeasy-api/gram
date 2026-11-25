/**
 * Restyles Elements' thread (which renders in a shadow root) toward the
 * Dex-like look of the prototype: the conversation sits on the page ground,
 * your messages are small tinted blocks, replies are plain prose, and the
 * composer is a flat bordered box. Only Elements' `aui-*` hooks are touched,
 * so streaming, reasoning, tool groups and scrolling stay Elements'.
 * Dashboard color tokens are custom properties, which inherit into the
 * shadow root, so they are used directly.
 */
export const PROTOTYPE_CHAT_CSS = `
  .aui-root, .aui-thread-viewport {
    background: transparent;
  }
  .aui-thread-viewport {
    padding-left: 0.25rem;
    padding-right: 0.25rem;
    /* Messages fade out as they scroll up under the top edge. */
    -webkit-mask-image: linear-gradient(to bottom, transparent 0, black 96px);
    mask-image: linear-gradient(to bottom, transparent 0, black 96px);
  }

  /* Your messages: a small tinted block on the right. */
  .aui-user-message-content {
    background: var(--bg-surface-tertiary-default);
    color: var(--foreground);
    border-radius: 0;
    padding: 0.375rem 0.75rem;
    font-size: 0.875rem;
    line-height: 1.5;
  }
  .aui-user-message-owner { display: none; }

  /* Replies: plain prose at app scale. */
  .aui-assistant-message-content {
    font-size: 0.9375rem;
    line-height: 1.65;
    color: var(--foreground);
  }
  .aui-md { font-size: 0.9375rem; line-height: 1.65; }
  .aui-md-h1 { font-size: 1.375rem; margin-bottom: 0.75rem; }
  .aui-md-h2 { font-size: 1.1875rem; margin-top: 1.25rem; margin-bottom: 0.5rem; }
  .aui-md-h3 { font-size: 1.0625rem; margin-top: 1rem; margin-bottom: 0.5rem; }
  .aui-md-p { margin-top: 0.625rem; margin-bottom: 0.625rem; }
  .aui-md-ul, .aui-md-ol { margin-top: 0.625rem; margin-bottom: 0.625rem; }
  .aui-md table { display: block; overflow-x: auto; max-width: 100%; }
  .gram-elements a.text-link-primary { color: #1e5aae; }
  .gram-elements.dark a.text-link-primary,
  .dark .gram-elements a.text-link-primary { color: #619aea; }

  /* Welcome: no greeting, just one row of suggestions above the composer. */
  .aui-thread-welcome-root { justify-content: flex-end; padding-bottom: 0.75rem; }
  .aui-thread-welcome-center, .aui-thread-welcome-logo,
  .aui-thread-welcome-message { display: none !important; }
  .aui-thread-welcome-suggestions {
    display: flex !important;
    flex-direction: row !important;
    flex-wrap: nowrap !important;
    justify-content: flex-start !important;
    gap: 0.5rem;
    width: 100%;
    overflow-x: auto;
    scrollbar-width: none;
  }
  .aui-thread-welcome-suggestions > * { flex: 0 0 auto !important; width: auto !important; }
  .aui-thread-welcome-suggestion {
    width: auto !important;
    white-space: nowrap;
    background: var(--bg-surface-primary-default);
    border: 1px solid var(--border-neutral-softest);
    border-radius: 0;
    padding: 0.5rem 0.75rem;
    font-size: 0.875rem;
  }
  .aui-thread-welcome-suggestion:hover {
    border-color: var(--border-neutral-default);
  }

  /* Composer: flat bordered box pinned to the bottom. */
  .aui-composer-wrapper {
    background: transparent;
    padding-bottom: 0;
  }
  .aui-composer-root {
    border-radius: 0;
    border: 1px solid var(--border-neutral-softest);
    background: var(--bg-surface-primary-default);
    box-shadow: none;
    min-height: 5.5rem;
  }
  .aui-composer-root:focus-within {
    border-color: var(--border-neutral-default);
  }
  .aui-composer-send, .aui-composer-cancel { border-radius: 0; }
`;
