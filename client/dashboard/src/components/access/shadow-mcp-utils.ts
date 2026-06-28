export function formatShortDate(value: Date | string | undefined): string {
  if (!value) return "Never";

  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  }).format(new Date(value));
}
