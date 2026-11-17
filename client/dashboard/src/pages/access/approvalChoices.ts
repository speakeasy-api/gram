/** End of day is offered only while the day has this much left in it. */
const MIN_END_OF_DAY_LEAD_MS = 10 * 60 * 1000;

const HOUR_MS = 60 * 60 * 1000;

type Choice = { key: string; label: string; expiresAt: Date };

/**
 * The fixed windows an administrator can approve a request for: the same ones
 * the Slack card offers. End of day follows the administrator's own clock
 * here; Slack uses the requester's.
 */
export function approvalChoices(now: Date): Choice[] {
  const choices: Choice[] = [
    {
      key: "1h",
      label: "1 hour",
      expiresAt: new Date(now.getTime() + HOUR_MS),
    },
    {
      key: "2h",
      label: "2 hours",
      expiresAt: new Date(now.getTime() + 2 * HOUR_MS),
    },
    {
      key: "4h",
      label: "4 hours",
      expiresAt: new Date(now.getTime() + 4 * HOUR_MS),
    },
  ];
  const endOfDay = new Date(now);
  endOfDay.setHours(23, 59, 0, 0);
  if (endOfDay.getTime() - now.getTime() >= MIN_END_OF_DAY_LEAD_MS) {
    choices.push({ key: "eod", label: "End of day", expiresAt: endOfDay });
  }
  return choices;
}
