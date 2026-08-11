import { useSyncExternalStore } from "react";
import { flushSync } from "react-dom";
import { useNavigate } from "react-router";
import { useInsightsState } from "@/components/insights-context";
import type {
  InsightsSuggestion,
  InsightsSuggestionIcon,
} from "@/lib/insights-suggestions";
import { useRoutes } from "@/routes";

/**
 * "Launch" animation for chat suggestion chips, in two View Transitions:
 *
 *  1. `chat-launch` — the clicked chip flies to the centre of the screen as a
 *     full-screen overlay card and spins a loader while the prompt is sent.
 *  2. `chat-launch-bubble` — the card reverse-genies into the blue user bubble
 *     of the conversation it started (keyframes in App.css).
 *
 * Two names rather than one so each half can have its own choreography, and
 * because a name may only ever exist on ONE element per snapshot — two
 * elements sharing a name abort the transition. Names are therefore applied
 * imperatively at the exact moment each snapshot is taken.
 *
 * The overlay lives in <AppLayout>, above the router outlet, so it survives
 * the navigation to `/chat/new` that happens mid-flight.
 */

/** Must match the ::view-transition-*() rules in App.css. */
const VT_ARRIVE = "chat-launch";

/** Marks the overlay card so the launcher can drive it without prop-drilling a
 *  ref out of the overlay component. */
export const CHAT_LAUNCH_CARD_ATTR = "data-chat-launch-card";
const CARD_SELECTOR = `[${CHAT_LAUNCH_CARD_ATTR}]`;
/** The card's icon and spinner — dropped early in the minimize, since the
 *  bubble has neither. */
export const CHAT_LAUNCH_ADORNMENT_ATTR = "data-chat-launch-adornment";
const ADORNMENT_SELECTOR = `[${CHAT_LAUNCH_ADORNMENT_ATTR}]`;
/** The dim + rainbow underlay. */
export const CHAT_LAUNCH_VEIL_ATTR = "data-chat-launch-veil";
const VEIL_SELECTOR = `[${CHAT_LAUNCH_VEIL_ATTR}]`;
/** The card's label — carries the suggestion title down, then swaps to the
 *  full prompt once the card has landed. */
export const CHAT_LAUNCH_TEXT_ATTR = "data-chat-launch-text";
const TEXT_SELECTOR = `[${CHAT_LAUNCH_TEXT_ATTR}]`;

/** Sweeps a highlight across the label while it swaps text (see App.css). */
const SHIMMER_CLASS = "chat-launch-shimmer";
/** Set on <html> for the duration of the arriving transition so its App.css
 *  rules only suppress the page cross-fade for this one transition. */
const LAUNCHING_CLASS = "chat-launching";

const FLIGHT_MS = 560;
const FLIGHT_EASING = "cubic-bezier(0.62, 0, 0.2, 1)";
/** Beat after landing, before the text starts changing. */
const LAND_HOLD_MS = 140;
const TEXT_OUT_MS = 130;
const TEXT_IN_MS = 220;
/** Tail of the shimmer sweep left to play before handing off. */
const SHIMMER_TAIL_MS = 260;
/** Cross-fade onto the real bubble, which by then holds identical text. */
const HANDOFF_MS = 160;

/** Extra beat after the chip has finished flying in, so the loader reads as a
 *  deliberate pause rather than a flicker. */
const LOADER_MIN_MS = 400;
/** Give up waiting for the bubble (assistant cold start, send error) and just
 *  drop the overlay. Generous: a cold runtime can take tens of seconds. */
const BUBBLE_TIMEOUT_MS = 30000;
/** Each poll walks open shadow trees, so keep the cadence modest. */
const BUBBLE_POLL_MS = 120;

/** The blue user bubble rendered by the Elements thread. */
const USER_BUBBLE_SELECTOR = '[data-role="user"] .aui-user-message-content';

export interface ChatLaunch {
  title: string;
  icon: InsightsSuggestionIcon;
  prompt: string;
}

// Module-level store rather than context: the chip (a page under the outlet)
// and the overlay (mounted in the layout shell) are in different subtrees, and
// the chip unmounts the moment the navigation lands.
let current: ChatLaunch | null = null;
const listeners = new Set<() => void>();

function setLaunch(next: ChatLaunch | null): void {
  current = next;
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** The in-flight launch, or null. Read by <ChatLaunchOverlay>. */
export function useChatLaunchState(): ChatLaunch | null {
  return useSyncExternalStore(
    subscribe,
    () => current,
    () => null,
  );
}

function canMorph(): boolean {
  return (
    typeof document !== "undefined" &&
    typeof document.startViewTransition === "function" &&
    !window.matchMedia?.("(prefers-reduced-motion: reduce)").matches
  );
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}

/** Elements renders the thread inside a shadow root (see ShadowRoot.tsx), so a
 *  plain `document.querySelector` never sees the bubble — walk open shadow
 *  trees too. `view-transition-name` still works across the boundary: the
 *  pseudo-elements it generates live on the document root either way. */
function deepQuery(
  selector: string,
  root: Document | ShadowRoot = document,
): HTMLElement | null {
  const direct = root.querySelector<HTMLElement>(selector);
  if (direct) return direct;
  for (const element of root.querySelectorAll<HTMLElement>("*")) {
    if (!element.shadowRoot) continue;
    const nested = deepQuery(selector, element.shadowRoot);
    if (nested) return nested;
  }
  return null;
}

/** Polls for the user bubble carrying `prompt`. The launched message is the
 *  first user turn of a fresh conversation, so the first match wins; the text
 *  check guards against matching a stale thread that hasn't swapped yet. It is
 *  a prefix match because the renderer may append trailing nodes to the text. */
async function waitForBubble(prompt: string): Promise<HTMLElement | null> {
  const deadline = Date.now() + BUBBLE_TIMEOUT_MS;
  while (Date.now() < deadline) {
    const bubble = deepQuery(USER_BUBBLE_SELECTOR);
    if (bubble?.textContent?.trim().startsWith(prompt.slice(0, 40))) {
      return bubble;
    }
    await delay(BUBBLE_POLL_MS);
  }
  return null;
}

/**
 * Second half: hold the loader until the bubble exists, then minimize the card
 * into it — a FLIP animation on the card itself rather than a second View
 * Transition. It has to be FLIP: the bubble lives inside the Elements shadow
 * root, and `view-transition-name` on a shadow-tree element produces no
 * pseudo-element at all in Chrome, so the browser would pair the card with
 * nothing and simply fade it out.
 *
 * The card is animated to the bubble's exact box while its background, text
 * colour and radius interpolate to the bubble's computed values, so the chip
 * styling becomes bubble styling on the way down. The real bubble is held
 * invisible underneath until the card lands on top of it.
 */
async function morphIntoBubble(prompt: string): Promise<void> {
  const bubble = await waitForBubble(prompt);
  await delay(LOADER_MIN_MS);

  const card = document.querySelector<HTMLElement>(CARD_SELECTOR);
  if (!bubble || !card || !canMorph()) {
    if (import.meta.env.DEV && !bubble) {
      console.warn("[chat-launch] no user bubble found; skipping the morph");
    }
    setLaunch(null);
    return;
  }

  const from = card.getBoundingClientRect();
  const to = bubble.getBoundingClientRect();
  const target = getComputedStyle(bubble);
  const origin = getComputedStyle(card);

  // Animate the card's real box rather than a transform: the label has to stay
  // legible after it lands (it still has a text swap to perform), and a
  // non-uniform scale would leave it stretched. Pin the card at its current
  // position first so leaving the centring flex layout doesn't move it.
  Object.assign(card.style, {
    position: "fixed",
    margin: "0",
    left: `${from.left}px`,
    top: `${from.top}px`,
    width: `${from.width}px`,
    height: `${from.height}px`,
    // The card is `max-w-xl` while it's a centred card; that cap would clamp
    // the flight short of a wider bubble and re-wrap the message.
    maxWidth: "none",
  });

  // Hide the real bubble so it isn't a second copy of itself mid-flight.
  bubble.style.opacity = "0";

  // The icon and spinner have no counterpart in a chat bubble.
  for (const adornment of card.querySelectorAll<HTMLElement>(
    ADORNMENT_SELECTOR,
  )) {
    adornment.animate([{ opacity: 1 }, { opacity: 0 }], {
      duration: FLIGHT_MS * 0.35,
      easing: "ease-out",
      fill: "forwards",
    });
  }

  document
    .querySelector<HTMLElement>(VEIL_SELECTOR)
    ?.animate([{ opacity: 1 }, { opacity: 0 }], {
      duration: FLIGHT_MS,
      easing: "ease-in",
      fill: "forwards",
    });

  const flight = card.animate(
    [
      {
        left: `${from.left}px`,
        top: `${from.top}px`,
        width: `${from.width}px`,
        height: `${from.height}px`,
        padding: origin.padding,
        fontSize: origin.fontSize,
        backgroundColor: origin.backgroundColor,
        color: origin.color,
        borderRadius: origin.borderRadius,
        borderColor: origin.borderColor,
        borderWidth: origin.borderWidth,
      },
      {
        left: `${to.left}px`,
        top: `${to.top}px`,
        width: `${to.width}px`,
        height: `${to.height}px`,
        padding: target.padding,
        fontSize: target.fontSize,
        backgroundColor: target.backgroundColor,
        color: target.color,
        borderRadius: target.borderRadius,
        borderColor: target.backgroundColor,
        // The bubble has no border. Two pixels of card border shrink the
        // content box just enough to wrap a message that fits the bubble on
        // one line, so the border has to go, not just change colour.
        borderWidth: "0px",
      },
    ],
    { duration: FLIGHT_MS, easing: FLIGHT_EASING, fill: "forwards" },
  );
  await flight.finished.catch(() => undefined);
  // Bake the flight's end state into inline styles so the follow loop below
  // can drive the box directly — a forwards-filled animation outranks inline
  // styles and would ignore every write.
  flight.commitStyles();
  flight.cancel();

  // From here the card shadows the bubble frame by frame. The transcript is
  // still settling — the thread column widens as the assistant's turn renders,
  // which can nearly double the bubble's width and re-wrap its text — so a
  // one-shot re-measure always loses the race. The full message has to wrap
  // exactly as it does in the bubble, or it spills out of the card.
  const stopFollowing = followBubble(card, bubble);

  await swapToFullPrompt(card, prompt, target);

  // Hand off: the bubble underneath now holds the same text in the same box,
  // so fading the card out is invisible.
  bubble.style.opacity = "";
  await card
    .animate([{ opacity: 1 }, { opacity: 0 }], {
      duration: HANDOFF_MS,
      easing: "linear",
      fill: "forwards",
    })
    .finished.catch(() => undefined);
  stopFollowing();
  setLaunch(null);
}

/** Pins the card to the bubble's box every frame until the returned stopper is
 *  called. Cheap — one rect read and four style writes per frame, for under a
 *  second. */
function followBubble(card: HTMLElement, bubble: HTMLElement): () => void {
  let frame = 0;
  const step = () => {
    const rect = bubble.getBoundingClientRect();
    card.style.left = `${rect.left}px`;
    card.style.top = `${rect.top}px`;
    card.style.width = `${rect.width}px`;
    card.style.height = `${rect.height}px`;
    frame = requestAnimationFrame(step);
  };
  step();
  return () => cancelAnimationFrame(frame);
}

/**
 * With the card sitting in the bubble's box — still showing the short
 * suggestion title — dissolve the title, put the full message in its place,
 * and sweep a shimmer across it so the substitution reads as the message
 * resolving rather than as a cut.
 *
 * The card also adopts the bubble's typography here, while the label is
 * invisible: the thread renders inside a shadow root with its own font stack
 * and line height, so keeping the dashboard's would wrap the message
 * differently from the bubble it is about to become.
 */
async function swapToFullPrompt(
  card: HTMLElement,
  prompt: string,
  target: CSSStyleDeclaration,
): Promise<void> {
  const label = card.querySelector<HTMLElement>(TEXT_SELECTOR);
  if (!label) return;

  await delay(LAND_HOLD_MS);
  await label
    .animate([{ opacity: 1 }, { opacity: 0.15 }], {
      duration: TEXT_OUT_MS,
      easing: "ease-in",
      fill: "forwards",
    })
    .finished.catch(() => undefined);

  // The overlay unmounts moments later, so writing through React here is safe.
  label.textContent = prompt;
  label.classList.add(SHIMMER_CLASS);
  // The adornments are invisible by now but still occupy layout; once the card
  // stops being a flex row they would stack above the label and push the
  // message out of the box. Take them out entirely.
  for (const adornment of card.querySelectorAll<HTMLElement>(
    ADORNMENT_SELECTOR,
  )) {
    adornment.style.display = "none";
  }

  // Match the bubble's own text layout, and drop the flex centring the icon
  // and spinner needed — a multi-line message has to start at the top of the
  // box the way the bubble's does.
  Object.assign(card.style, {
    display: "block",
    fontFamily: target.fontFamily,
    fontSize: target.fontSize,
    lineHeight: target.lineHeight,
    fontWeight: target.fontWeight,
    letterSpacing: target.letterSpacing,
  });

  await label
    .animate([{ opacity: 0.15 }, { opacity: 1 }], {
      duration: TEXT_IN_MS,
      easing: "ease-out",
      fill: "forwards",
    })
    .finished.catch(() => undefined);
  await delay(SHIMMER_TAIL_MS);
}

/**
 * Returns a launcher for suggestion chips. Pass the clicked element so it can
 * be tagged as the transition's starting geometry; without it (or without View
 * Transitions support / with reduced motion) the prompt is sent with no
 * animation.
 */
export function useChatLaunch(): (
  suggestion: InsightsSuggestion,
  source: HTMLElement | null,
) => void {
  const navigate = useNavigate();
  const routes = useRoutes();
  const { sendPrompt } = useInsightsState();

  return (suggestion, source) => {
    const prompt = suggestion.prompt.trim();
    if (!prompt) return;

    // Start the conversation on the shared runtime, then drop into the
    // full-page view — the queued prompt fires once the chat route mounts the
    // runtime. The server mints the chat id on the first send.
    const send = () => {
      sendPrompt(prompt);
      void navigate(routes.chat.conversation.href("new"));
    };

    if (!source || !canMorph()) {
      send();
      return;
    }

    source.style.viewTransitionName = VT_ARRIVE;
    // Scopes the "no page cross-fade" rules in App.css to this transition.
    document.documentElement.classList.add(LAUNCHING_CLASS);
    const transition = document.startViewTransition(() => {
      // Sending inside the same flush keeps it one atomic DOM change: the old
      // snapshot is the chip, the new one is the overlay over the chat page.
      flushSync(() => {
        setLaunch({
          title: suggestion.title,
          icon: suggestion.icon ?? "sparkles",
          prompt,
        });
        send();
      });
      // react-router defers the route swap past this flush, so the chip is
      // still mounted when the new snapshot is taken — drop its name here or
      // the browser sees two `chat-launch` elements and aborts.
      source.style.viewTransitionName = "";
      const card = document.querySelector<HTMLElement>(CARD_SELECTOR);
      if (card) card.style.viewTransitionName = VT_ARRIVE;
    });

    // Wait for the flight to land before starting the second transition — an
    // overlapping one would abort it mid-air.
    void transition.finished
      .catch(() => undefined)
      .then(() => {
        document.documentElement.classList.remove(LAUNCHING_CLASS);
        return morphIntoBubble(prompt);
      });
  };
}
