import type { IdentityRailItem } from "@/components/identity-rail";
import type { useRoutes } from "@/routes";

/**
 * The identity page's sub-pages, in reading order: what needs attention first,
 * then the subsystems that explain it.
 *
 * Active comes from the route table's own matcher rather than from a path
 * comparison: the URN segment is url-encoded in the href and decoded in the
 * location, so a literal match on the colon in `user:...` never lands.
 */
export function identityRailItems(
  routes: ReturnType<typeof useRoutes>,
  encodedUrn: string,
  /**
   * The current query string, carried onto every rail link: the window and the
   * project live in the URL, so without it moving between sub-pages silently
   * resets the filters the reader just set.
   */
  search = "",
): IdentityRailItem[] {
  const detail = routes.identities.detail;
  return [
    {
      key: "overview",
      title: "Overview",
      href: `${detail.overview.href(encodedUrn)}${search}`,
      active: detail.overview.active,
    },
    {
      key: "access",
      title: "Access",
      href: `${detail.access.href(encodedUrn)}${search}`,
      active: detail.access.active,
    },
    {
      key: "usage",
      title: "Usage",
      href: `${detail.usage.href(encodedUrn)}${search}`,
      active: detail.usage.active,
    },
    {
      key: "security",
      title: "Security",
      href: `${detail.security.href(encodedUrn)}${search}`,
      active: detail.security.active,
    },
    {
      key: "cost",
      title: "Cost",
      href: `${detail.cost.href(encodedUrn)}${search}`,
      active: detail.cost.active,
    },
    {
      key: "connections",
      title: "Connections",
      href: `${detail.connections.href(encodedUrn)}${search}`,
      active: detail.connections.active,
    },
    {
      // "Devices" undersold the tab once the linked provider accounts moved
      // in beside them: both answer the same question — the machines and the
      // logins this person works through.
      key: "devices",
      title: "Accounts & devices",
      href: `${detail.devices.href(encodedUrn)}${search}`,
      active: detail.devices.active,
    },
    {
      key: "activity",
      title: "Activity",
      href: `${detail.activity.href(encodedUrn)}${search}`,
      active: detail.activity.active,
    },
  ];
}
