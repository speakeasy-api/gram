import { AccountRow } from "@/components/observe/account-display";
import { cn } from "@/lib/utils";
import { PERSONAL_ACCOUNT_GOVERNANCE_NOTE } from "@/lib/personal-account-governance";
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
import {
  retryFailed,
  useIdentityDevices,
  useIdentityIsKnown,
  useIdentityPeers,
  useIdentityWindow,
} from "./useIdentityQueries";

/**
 * The platform mark a device row carries, keyed on the OS the MDM reports.
 *
 * The Apple mark is monochrome black and disappears on a dark background, so
 * it is the only one flipped; the Windows and Tux marks are coloured and must
 * not be. An OS with no mark of ours renders nothing rather than a stand-in.
 */
const OS_LOGOS: Record<string, { src: string; invertInDark: boolean }> = {
  macos: { src: "/icons/platforms/macos.svg", invertInDark: true },
  windows: { src: "/icons/platforms/windows.svg", invertInDark: false },
  linux: { src: "/icons/platforms/linux.svg", invertInDark: false },
};

function osLogo(osName: string | undefined): JSX.Element | undefined {
  const logo = OS_LOGOS[(osName ?? "").trim().toLowerCase()];
  if (!logo) return undefined;
  return (
    <img
      src={logo.src}
      alt=""
      aria-hidden
      className={cn(
        "size-4 shrink-0 object-contain",
        logo.invertInDark && "dark:invert",
      )}
    />
  );
}

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
  const routes = useRoutes();
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
  // One capped page: a cursor means MDM holds more machines than are listed
  // below, so every count drawn from this list is a floor.
  const devicesTruncated = !!devicesQuery.data?.result.nextCursor;
  const deviceCount = devicesTruncated
    ? `${devices.length.toLocaleString()} or more`
    : devices.length.toLocaleString();

  // Linked accounts ride on the roster row, not the per-user summary, so this
  // is the same read the Usage tab already makes for its agent surfaces.
  const { from, to } = useIdentityWindow();
  const {
    self,
    isPending: peersPending,
    isError: peersFailed,
    refetch: refetchPeers,
  } = useIdentityPeers(identity, from, to);
  const peersRetry = { isError: peersFailed, refetch: refetchPeers };
  const retryPeers = retryFailed(peersRetry);
  const accounts = (self?.accounts ?? []).map((account) => ({
    email: account.email ?? "",
    provider: account.provider,
    accountType: account.accountType ?? "",
  }));
  const personalCount = accounts.filter(
    (account) => account.accountType === "personal",
  ).length;

  // Enrollment, spelled out rather than asserted. The identities list reduces
  // it to one word per row, and the word carries no definition — someone is
  // "enrolled" once the directory knows them AND we have seen them work. Each
  // step below is one half of that, plus the two things that make the rest of
  // this tab worth reading.
  const hasDirectoryRow = identity.userIds.length > 0;
  // All-time, not the page's window: enrollment is a standing fact about a
  // person, and the identities list reports it that way. Reading it off the
  // windowed roster marked someone "not enrolled" for having been quiet for
  // the last seven days.
  const knownAllTime = useIdentityIsKnown(identity);
  const seenWorking = knownAllTime.known;
  const enrolled = hasDirectoryRow && seenWorking;
  const enrollmentSteps: {
    key: string;
    title: string;
    detail: string;
    met: boolean;
  }[] = [
    {
      key: "directory",
      title: "Known to the directory",
      detail: hasDirectoryRow
        ? "Has an account in this organization."
        : "Seen in telemetry, but matches no member of this organization.",
      met: hasDirectoryRow,
    },
    {
      key: "activity",
      title: "Seen working",
      detail: seenWorking
        ? "Has recorded tool or agent-session activity."
        : "No tool calls or agent sessions recorded yet.",
      met: seenWorking,
    },
    {
      key: "accounts",
      title: "Linked to an AI account",
      detail:
        accounts.length > 0
          ? `${accounts.length} account${accounts.length === 1 ? "" : "s"} linked automatically from activity.`
          : "No provider account linked from activity yet.",
      met: accounts.length > 0,
    },
    {
      key: "devices",
      title: "On a managed device",
      detail:
        devices.length > 0
          ? `${deviceCount} device${devices.length === 1 && !devicesTruncated ? "" : "s"} assigned in MDM.`
          : "No managed device assigned in MDM.",
      met: devices.length > 0,
    },
  ];

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
      title="Accounts & devices"
      meta={sectionMeta([
        { count: accounts.length, singular: "account" },
        { count: devices.length, singular: "device" },
      ])}
    >
      <div className="flex flex-col gap-4">
        {/* Enrollment leads, because it is the reading the identities list
            shows as a single word and never defines. */}
        <IdentityPanel
          title="Enrollment"
          loading={
            peersPending || devicesQuery.isLoading || knownAllTime.isPending
          }
          // Every step below reads off data that failed to arrive as a "No",
          // and the footer then concludes "Not enrolled" — the worst possible
          // wrong answer on this page.
          error={peersFailed || devicesQuery.isError || knownAllTime.isError}
          onRetry={retryFailed(peersRetry, devicesQuery, knownAllTime)}
          footer={
            enrolled
              ? "Enrolled: the directory knows this person and we have seen them work."
              : "Not enrolled: enrollment needs both a directory account and recorded activity."
          }
        >
          {enrollmentSteps.map((step) => (
            <IdentityPanelRow
              key={step.key}
              accent={step.met ? undefined : "warning"}
              title={step.title}
              detail={step.detail}
              trailing={step.met ? "Yes" : "No"}
            />
          ))}
        </IdentityPanel>

        {/* Accounts lead: which logins someone works through is the question a
            personal subscription answers wrongly, and a machine with no agent
            is the same question asked of hardware. */}
        <IdentityPanel
          title="Linked accounts"
          loading={peersPending}
          error={peersFailed && accounts.length === 0}
          refreshFailed={peersFailed && accounts.length > 0}
          onRetry={retryPeers}
          footer={
            accounts.length > 0
              ? `${PERSONAL_ACCOUNT_GOVERNANCE_NOTE}${
                  personalCount > 0
                    ? ` ${personalCount} of these ${
                        personalCount === 1 ? "is" : "are"
                      } personal.`
                    : ""
                }`
              : undefined
          }
        >
          {accounts.length === 0 ? (
            <IdentityPanelEmpty>
              No AI accounts linked yet. As this identity is seen using AI tools
              (Claude, Codex, Cursor), the team and personal accounts they sign
              in with are linked automatically and appear here.
            </IdentityPanelEmpty>
          ) : (
            <div className="flex flex-col gap-3 px-4 py-4">
              {accounts.map((account, index) => (
                <AccountRow
                  key={`${account.provider}:${account.email}:${index}`}
                  account={account}
                />
              ))}
            </div>
          )}
        </IdentityPanel>

        <IdentityPanel
          title="Managed devices"
          handoffLabel="Device Agent"
          handoffHref={handoffs.deviceAgent}
          loading={devicesQuery.isLoading}
          error={devicesQuery.isError && devices.length === 0}
          refreshFailed={devicesQuery.isError && devices.length > 0}
          onRetry={retryFailed(devicesQuery)}
          footer={[
            coverageNote,
            // Devices match on the id OR on the MDM-reported email, because a
            // device only carries a user id when that email resolved.
            identity.emails.length > 0
              ? `Matched on ${identity.userIds.length} user id${
                  identity.userIds.length === 1 ? "" : "s"
                } and ${identity.emails.length} address${
                  identity.emails.length === 1 ? "" : "es"
                }.`
              : "",
            devicesTruncated
              ? `Showing the first ${devices.length.toLocaleString()}; this identity has more assigned.`
              : "",
          ]
            .filter(Boolean)
            .join(" ")}
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
                  leading={osLogo(device.osName)}
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
      </div>
    </IdentitySection>
  );
}
