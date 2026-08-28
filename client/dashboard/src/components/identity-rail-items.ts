import type { IdentityRailItem } from "@/components/identity-rail";
import type { useRoutes } from "@/routes";

/**
 * The identity page's sub-pages, in reading order: what needs attention first,
 * then the subsystems that explain it.
 *
 * Active comes from the route table rather than from NavLink's own matching:
 * the URN segment is url-encoded, and NavLink compares the decoded pathname, so
 * the colon in `user:...` never matches.
 */
export function identityRailItems(
  routes: ReturnType<typeof useRoutes>,
  encodedUrn: string,
): IdentityRailItem[] {
  const detail = routes.identities;
  return [
    {
      key: "overview",
      title: "Overview",
      href: detail.overview.href(encodedUrn),
      active: detail.overview.active,
    },
    {
      key: "access",
      title: "Access",
      href: detail.access.href(encodedUrn),
      active: detail.access.active,
    },
    {
      key: "usage",
      title: "Usage",
      href: detail.usage.href(encodedUrn),
      active: detail.usage.active,
    },
    {
      key: "security",
      title: "Security",
      href: detail.security.href(encodedUrn),
      active: detail.security.active,
    },
    {
      key: "cost",
      title: "Cost",
      href: detail.cost.href(encodedUrn),
      active: detail.cost.active,
    },
    {
      key: "devices",
      title: "Devices",
      href: detail.devices.href(encodedUrn),
      active: detail.devices.active,
    },
    {
      key: "activity",
      title: "Activity",
      href: detail.activity.href(encodedUrn),
      active: detail.activity.active,
    },
  ];
}
