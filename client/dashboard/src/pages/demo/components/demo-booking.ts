// Cal.com event link booked when an org isn't whitelisted yet — the calendar is
// embedded directly (no routing form). Rotate here if the Cal event changes.
export const CAL_DEMO_LINK = "team/speakeasy-com/ai-transformation";

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
