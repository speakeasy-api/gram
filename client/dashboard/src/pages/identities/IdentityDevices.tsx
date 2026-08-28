import { HumanizeDateTime } from "@/lib/dates";
import { useOrgRoutes } from "@/routes";
import {
  IdentityPanel,
  IdentityPanelEmpty,
  IdentityPanelRow,
} from "./IdentityPanel";
import { useIdentityOutlet } from "./identityRoute";
import { IdentitySection } from "./IdentitySection";
import { useIdentityDevices } from "./useIdentityQueries";

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
  const { identity } = useIdentityOutlet();
  const orgRoutes = useOrgRoutes();
  const devicesQuery = useIdentityDevices(identity);
  const devices = devicesQuery.data?.result.devices ?? [];

  return (
    <IdentitySection
      title="Devices"
      meta={`${devices.length} device${devices.length === 1 ? "" : "s"}`}
    >
      <IdentityPanel
        title="Managed devices"
        handoffLabel="Device Agent"
        handoffHref={orgRoutes.deviceAgent.href()}
        footer={
          // Devices match on the id OR on the MDM-reported email, because a
          // device only carries a user id when that email resolved.
          identity.emails.length > 0
            ? `Matched on ${identity.userIds.length} user id${
                identity.userIds.length === 1 ? "" : "s"
              } and ${identity.emails.length} address${
                identity.emails.length === 1 ? "" : "es"
              }`
            : undefined
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
