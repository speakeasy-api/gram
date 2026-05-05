/** Common values surfaced in the edit modals' dropdowns. The dev-idp accepts
 *  any string for these fields, so the dropdown also passes through whatever
 *  was previously stored (rendered alongside these defaults). */

export const ACCOUNT_TYPES = ["free", "pro", "enterprise"] as const;

export const ROLE_OPTIONS = [
  "admin",
  "member",
  "advisor",
  "consultant",
  "intern",
] as const;
