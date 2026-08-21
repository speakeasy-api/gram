/**
 * Pylon widget initialization and chat-visibility tracking.
 *
 * Pylon's widget reads `window.pylon.chat_settings` once on script
 * execution to associate the visitor with their persisted thread history.
 * If chat_settings isn't present when the script runs, the visitor is
 * treated as anonymous and starts a fresh thread on every load —
 * which is why we must set chat_settings *before* injecting the script.
 *
 * The default launcher bubble is hidden via CSS — chat is opened from
 * the account menu (or `showPylonChat()` from other buttons). Visibility
 * is tracked globally via Pylon's `onShow` / `onHide` so the menu label
 * stays in sync with the widget's own close control.
 */

export const PYLON_APP_ID = "f9cade16-8d3c-4826-9a2a-034fad495102";

export type PylonChatSettings = {
  app_id: string;
  email: string;
  name: string;
  avatar_url?: string;
  email_hash?: string;
  hide_default_launcher?: boolean;
};

declare global {
  interface Window {
    Pylon: ((action: string, ...args: unknown[]) => void) & {
      q: unknown[];
      e: (args: unknown) => void;
    };
    pylon?: {
      chat_settings: PylonChatSettings;
    };
  }
}

let initialized = false;
let chatOpen = false;
const chatListeners = new Set<() => void>();

function setChatOpen(next: boolean): void {
  if (chatOpen === next) {
    return;
  }
  chatOpen = next;
  for (const listener of chatListeners) {
    listener();
  }
}

export function isPylonChatOpen(): boolean {
  return chatOpen;
}

export function subscribePylonChatOpen(listener: () => void): () => void {
  chatListeners.add(listener);
  return () => {
    chatListeners.delete(listener);
  };
}

/**
 * Attach onShow/onHide to the current `window.Pylon`. Safe to call
 * repeatedly: the first bind often hits the pre-script queue stub, and
 * Pylon keeps only one callback per event (last registration wins).
 */
export function bindPylonChatListeners(): void {
  if (typeof window.Pylon !== "function") {
    return;
  }
  window.Pylon("onShow", () => {
    setChatOpen(true);
  });
  window.Pylon("onHide", () => {
    setChatOpen(false);
  });
}

export function showPylonChat(): void {
  bindPylonChatListeners();
  window.Pylon?.("show");
}

export function hidePylonChat(): void {
  bindPylonChatListeners();
  window.Pylon?.("hide");
}

export function togglePylonChat(): void {
  if (chatOpen) {
    hidePylonChat();
  } else {
    showPylonChat();
  }
}

/**
 * Initialize the Pylon widget. Idempotent — subsequent calls update
 * `window.pylon.chat_settings` so re-identification reflects the latest
 * user data but never injects a second script tag.
 */
export function initializePylon(chatSettings: PylonChatSettings): void {
  // Always keep chat_settings in sync with the latest user identity.
  window.pylon = { chat_settings: chatSettings };

  if (initialized) {
    bindPylonChatListeners();
    return;
  }
  initialized = true;

  const style = document.createElement("style");
  style.textContent = `#pylon-chat-bubble { display: none !important; }`;
  document.head.appendChild(style);

  const queue: unknown[] = [];
  const enqueue = (args: unknown) => {
    queue.push(args);
  };

  const pylonFn = function (this: unknown, ...args: unknown[]) {
    enqueue(args);
  } as typeof window.Pylon;

  pylonFn.q = queue;
  pylonFn.e = enqueue;

  window.Pylon = pylonFn;
  bindPylonChatListeners();

  const script = document.createElement("script");
  script.setAttribute("type", "text/javascript");
  script.setAttribute("async", "true");
  script.setAttribute(
    "src",
    `https://widget.usepylon.com/widget/${PYLON_APP_ID}`,
  );

  const firstScript = document.getElementsByTagName("script")[0];
  firstScript?.parentNode?.insertBefore(script, firstScript);
}
