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
  /** Whether the viewer may open Findings; the entry is left out otherwise. */
  showFindings = false,
  /**
   * An agent is not a person, and three of the people tabs have nothing to
   * read for one: usage, cost and risk findings are all keyed by a human
   * subject, so each rendered a panel whose only content was a sentence
   * explaining that it had no content. A tab that can never fill is worse
   * than a missing one — it costs a click to learn nothing.
   */
  kind?: string,
): IdentityRailItem[] {
  const detail = routes.identities.detail;
  if (kind === "agent") {
    return [
      {
        key: "overview",
        title: "Overview",
        href: `${detail.overview.href(encodedUrn)}${search}`,
        active: detail.overview.active,
      },
      {
        key: "permissions",
        title: "Permissions",
        href: `${detail.permissions.href(encodedUrn)}${search}`,
        active: detail.permissions.active,
      },
      {
        // How the agent is stood up: the credential it authenticates with,
        // and what hands that credential to its runtime. It holds no provider
        // logins and no managed machines, so this is not "accounts & devices"
        // for an agent, and "Keys" named the artefact rather than the job.
        key: "provisioning",
        title: "Provisioning",
        href: `${detail.provisioning.href(encodedUrn)}${search}`,
        active: detail.provisioning.active,
      },
      {
        key: "sessions",
        title: "Sessions",
        href: `${detail.sessions.href(encodedUrn)}${search}`,
        active: detail.sessions.active,
      },
      {
        key: "activity",
        title: "Activity",
        href: `${detail.activity.href(encodedUrn)}${search}`,
        active: detail.activity.active,
      },
    ];
  }
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
    ...(showFindings
      ? [
          {
            key: "findings",
            title: "Findings",
            href: `${detail.findings.href(encodedUrn)}${search}`,
            active: detail.findings.active,
            nested: true,
          },
        ]
      : []),
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
