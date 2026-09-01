// Cal.com event link booked when an org isn't whitelisted yet — the calendar is
// embedded directly (no routing form). Rotate here if the Cal event changes.
export const CAL_DEMO_LINK = "team/speakeasy-com/ai-transformation";

// Same event, opened as a normal page. The fallback for anyone whose embed
// never renders, so it must not depend on the embed script.
export const CAL_DEMO_URL = `https://cal.com/${CAL_DEMO_LINK}`;

export const SALES_EMAIL = "sales@speakeasy.com";

// Cal keys everything — the iframe, the queued instructions, the event
// listeners — by namespace, and the default namespace ("") is shared with any
// other Cal embed on the page. Naming ours keeps the `ui` instruction and the
// iframe on the same instance, which is what stops `ui` resolving against an
// instance that has no iframe of its own ("iframe doesn't exist.
// `createIframe` must be called before `doInIframe`").
export const CAL_DEMO_NAMESPACE = "gram-demo";

// How long to wait for Cal's `linkReady` before offering the fallback. Cal
// leaves the iframe `visibility: hidden` until that event fires, so until then
// there is nothing to see either way: a slow embed and a broken one both look
// like an empty box.
export const CAL_EMBED_TIMEOUT_MS = 12_000;

export function splitDisplayName(displayName?: string): {
  firstName: string;
  lastName: string;
} {
  const trimmed = (displayName ?? "").trim();
  if (!trimmed) return { firstName: "", lastName: "" };
  const spaceIndex = trimmed.indexOf(" ");
  if (spaceIndex === -1) return { firstName: trimmed, lastName: "" };
  return {
    firstName: trimmed.slice(0, spaceIndex),
    lastName: trimmed.slice(spaceIndex + 1).trim(),
  };
}
