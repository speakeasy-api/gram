import { HumanizeDateTime } from "@/lib/dates";
import { useLocation } from "react-router";
import { useOrgRoutes, useRoutes } from "@/routes";
import {
  IdentityPanel,
  IdentityPanelEmpty,
  IdentityPanelRow,
} from "./IdentityPanel";
import { identityHandoffs } from "./identityHandoffs";
import { useIdentityOutlet } from "./identityRoute";
import { IdentitySection } from "./IdentitySection";
import { sectionMeta } from "./sectionMeta";
import { useIdentityDevices, useIdentityProject } from "./useIdentityQueries";

/** How each coverage bucket reads, and whether it is worth flagging. */
const COVERAGE: Record<string, { label: string; flag: boolean }> = {
  agent_active: { label: "Agent active", flag: false },
  agent_stale: { label: "Agent stale", flag: true },
  agent_other_device: { label: "Agent on another device", flag: false },
  no_agent: { label: "No agent", flag: true },
  no_email: { label: "No assigned email", flag: true },
  unresolved_email: { label: "Email unresolved", flag: true },
  missing: { label: "Missing from MDM", flag: true },
};

export default function IdentityDevices(): JSX.Element {
  const location = useLocation();
  const project = useIdentityProject();
  // Project routes resolve against the project this page is filtered to: the
  // page is org-level, so the router has no :projectSlug of its own to fill in
  // and every handoff would otherwise resolve to a path with the slug missing.
  const routes = useRoutes({ projectSlug: project.slug });
  const { identity } = useIdentityOutlet();
  const orgRoutes = useOrgRoutes();
  // No handoff on this page filters by principal, so the member list this
  // would otherwise fetch is not worth the request.
  const handoffs = identityHandoffs(
    identity,
    routes,
    orgRoutes,
    undefined,
    new URLSearchParams(location.search),
  );
  const devicesQuery = useIdentityDevices(identity);
  const devices = devicesQuery.data?.result.devices ?? [];

  // Deliberately outside the page's time range. Every other panel reports
  // events, which a window slices; this reports the MDM inventory as it stands
  // right now. A laptop is assigned or it is not — "devices in the last 7
  // days" would answer a question nobody asked and hide a machine that has
  // been quietly missing its agent for a month, which is exactly the one worth
  // seeing. The footer says so rather than leaving the reader to assume the
  // range applied here too.
  const coverageNote =
    "Current MDM inventory — not filtered by the selected time range.";

  return (
    <IdentitySection
      title="Devices"
      meta={sectionMeta([{ count: devices.length, singular: "device" }])}
    >
      <IdentityPanel
        title="Managed devices"
        handoffLabel="Device Agent"
        handoffHref={handoffs.deviceAgent}
        loading={devicesQuery.isLoading}
        footer={
          // Devices match on the id OR on the MDM-reported email, because a
          // device only carries a user id when that email resolved.
          identity.emails.length > 0
            ? `${coverageNote} Matched on ${identity.userIds.length} user id${
                identity.userIds.length === 1 ? "" : "s"
              } and ${identity.emails.length} address${
                identity.emails.length === 1 ? "" : "es"
              }.`
            : coverageNote
        }
      >
        {devices.length === 0 ? (
          <IdentityPanelEmpty>
            No managed device is assigned to this identity.
          </IdentityPanelEmpty>
        ) : (
          devices.map((device) => {
            const coverage = COVERAGE[device.coverageBucket];
            return (
              <IdentityPanelRow
                key={device.id}
                accent={coverage?.flag ? "warning" : undefined}
                title={
                  device.hostname ?? device.serialNumber ?? device.externalId
                }
                detail={[
                  device.osName && device.osVersion
                    ? `${device.osName} ${device.osVersion}`
                    : device.osName,
                  device.provider,
                ]
                  .filter(Boolean)
                  .join(" · ")}
                trailing={
                  device.agentLastSeenAt ? (
                    <HumanizeDateTime date={device.agentLastSeenAt} />
                  ) : (
                    (coverage?.label ?? device.coverageBucket)
                  )
                }
              />
            );
          })
        )}
      </IdentityPanel>
    </IdentitySection>
  );
}
