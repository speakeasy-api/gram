/** Only billing recovery for the active organization bypasses the lockout. */
export function isBillingRecoveryPath(
  pathname: string,
  search: string,
  organizationSlug: string | undefined,
): boolean {
  if (!organizationSlug) return false;
  pathname = pathname.replace(/\/$/, "");
  const billing = `/${organizationSlug}/billing`;
  return (
    pathname === `${billing}/setup` ||
    pathname === `${billing}/payment-method` ||
    (pathname === billing && new URLSearchParams(search).has("session_id"))
  );
}
