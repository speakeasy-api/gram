import type { IdentityRailItem } from "@/components/identity-rail";
import type { useRoutes } from "@/routes";

/**
 * The identity page's sub-pages, in reading order: what needs attention first,
 * then the subsystems that explain it.
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
    },
    { key: "access", title: "Access", href: detail.access.href(encodedUrn) },
    { key: "usage", title: "Usage", href: detail.usage.href(encodedUrn) },
    {
      key: "security",
      title: "Security",
      href: detail.security.href(encodedUrn),
    },
    { key: "cost", title: "Cost", href: detail.cost.href(encodedUrn) },
    { key: "devices", title: "Devices", href: detail.devices.href(encodedUrn) },
    {
      key: "activity",
      title: "Activity",
      href: detail.activity.href(encodedUrn),
    },
  ];
}
