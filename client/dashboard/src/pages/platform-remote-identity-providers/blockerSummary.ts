// blockerSummary explains which of the two client populations stand in the way
// of deleting a platform issuer, because only one of them is the platform
// admin's to clear. Global clients live in the catalog and can be deleted
// there. Tenant clients belong to organizations, never appear in any platform
// listing, and can only be removed by their owner — so a bare "delete the
// clients first" would point the admin at rows they cannot see. This mirrors
// the server's conflict message so the dialog says the same thing before the
// attempt that the API says after it.
export function blockerSummary(
  globalClientCount: number,
  tenantClientCount: number,
): string {
  if (globalClientCount === 0 && tenantClientCount === 0) {
    return "No clients are registered with this provider.";
  }

  const parts: string[] = [];
  if (globalClientCount > 0) {
    parts.push(
      `${globalClientCount} platform ${globalClientCount === 1 ? "client" : "clients"} (delete ${globalClientCount === 1 ? "it" : "them"} here first)`,
    );
  }
  if (tenantClientCount > 0) {
    parts.push(
      `${tenantClientCount} tenant-owned ${tenantClientCount === 1 ? "client" : "clients"} (only the owning ${tenantClientCount === 1 ? "organization" : "organizations"} can remove ${tenantClientCount === 1 ? "it" : "them"})`,
    );
  }

  return `Blocked by ${parts.join(" and ")}.`;
}
