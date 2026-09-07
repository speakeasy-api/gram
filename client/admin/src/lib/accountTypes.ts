// The list the admin API accepts.
export const ACCOUNT_TYPE_OPTIONS = [
  "free",
  "pro",
  "payg",
  "enterprise",
] as const;

export type AccountType = (typeof ACCOUNT_TYPE_OPTIONS)[number];

// An organization can still carry a value from outside the list. A select that
// shows one organization's own type therefore has to add that value as an extra
// option, or the trigger paints blank. A select that filters the list does not:
// it offers the list and nothing else.
export function isAccountType(value: unknown): value is AccountType {
  return ACCOUNT_TYPE_OPTIONS.some((option) => option === value);
}
