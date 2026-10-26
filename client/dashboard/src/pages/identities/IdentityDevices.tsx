import { IdentityWorkIdentities } from "./IdentityWorkIdentities";
import { AccountRow } from "@/components/observe/account-display";
import { Button } from "@/components/ui/Button";
import { cn } from "@/lib/utils";
import { PERSONAL_ACCOUNT_GOVERNANCE_NOTE } from "@/lib/personal-account-governance";
import { HumanizeDateTime } from "@/lib/dates";
import { ChevronDown, ChevronUp } from "lucide-react";
import { useState } from "react";
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
  useIdentityMember,
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
const OS_LOGOS = {
  macos: { src: "/icons/platforms/macos.svg", invertInDark: true },
  windows: { src: "/icons/platforms/windows.svg", invertInDark: false },
  linux: { src: "/icons/platforms/linux.svg", invertInDark: false },
} as const;

/**
 * Matched on a prefix, because the providers do not agree on the string: Jamf
 * and Kandji report the platform ("Mac"), Intune reports the operating system
 * ("macOS", "Windows"), and a version often rides along. An exact-match table
 * would leave whole fleets unmarked.
 */
function osLogo(osName: string | undefined): JSX.Element | undefined {
  const value = (osName ?? "").trim().toLowerCase();
  const key = value.startsWith("mac")
    ? "macos"
    : value.startsWith("windows")
      ? "windows"
      : value.startsWith("linux")
        ? "linux"
        : undefined;
  const logo = key ? OS_LOGOS[key] : undefined;
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
  // No handoff on this page filters by principal, so none needs the member
  // row.
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

  // Three separate facts, each answered on its own, the same three the
  // identities list filters on. None implies another: a directory profile can
  // belong to someone who never signed in, and activity can come from someone
  // with no account here. Then the two things that make the rest of this tab
  // worth reading.
  //
  // The member row is matched the way every other panel here matches it, so
  // "member" agrees with the access and challenge panels.
  const { member, query: membersQuery } = useIdentityMember(identity);
  const isMember = member !== undefined;
  const inDirectory = member?.inDirectory ?? false;
  // All-time, not the page's window: being seen is a standing fact about a
  // person, and the identities list reports it that way. Reading it off the
  // windowed roster marked someone unseen for having been quiet for the last
  // seven days.
  const knownAllTime = useIdentityIsKnown(identity);
  const seenWorking = knownAllTime.known;
  const statusSteps: {
    key: string;
    title: string;
    detail: string;
    met: boolean;
  }[] = [
    {
      key: "directory",
      title: "In directory",
      detail: inDirectory
        ? "Has a live profile in the organization's synced directory."
        : isMember
          ? "No live profile in a synced directory."
          : "Matches no member, so there is no directory profile to read.",
      met: inDirectory,
    },
    {
      key: "member",
      title: "Member",
      detail: isMember
        ? "Is a member of this organization."
        : "Matches no member of this organization.",
      met: isMember,
    },
    {
      key: "activity",
      title: "Seen",
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
  // The rows answer questions most visits do not ask, so the panel starts
  // folded to a one-line summary and opens on request.
  const [statusOpen, setStatusOpen] = useState(false);
  const metCount = statusSteps.filter((step) => step.met).length;

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
      timeRange
      meta={sectionMeta([
        { count: accounts.length, singular: "AI account" },
        { count: devices.length, singular: "device" },
      ])}
    >
      <div className="flex flex-col gap-4">
        <IdentityWorkIdentities identity={identity} />
        {/* The facts the identities list filters on, one row each. */}
        <IdentityPanel
          title="Status"
          action={
            <Button
              variant="tertiary"
              size="sm"
              aria-expanded={statusOpen}
              onClick={() => setStatusOpen((open) => !open)}
            >
              {statusOpen ? "Hide" : "Show"}
              {statusOpen ? (
                <ChevronUp className="size-4" aria-hidden="true" />
              ) : (
                <ChevronDown className="size-4" aria-hidden="true" />
              )}
            </Button>
          }
          loading={
            peersPending ||
            devicesQuery.isLoading ||
            knownAllTime.isPending ||
            membersQuery.isLoading
          }
          // Every row below reads data that failed to arrive as a "No", which
          // states something about the person that we do not know.
          error={
            peersFailed ||
            devicesQuery.isError ||
            knownAllTime.isError ||
            membersQuery.isError
          }
          onRetry={retryFailed(
            peersRetry,
            devicesQuery,
            knownAllTime,
            membersQuery,
          )}
          footer={
            statusOpen
              ? "Directory, membership and activity are separate facts. None of them implies another."
              : undefined
          }
        >
          {statusOpen ? (
            statusSteps.map((step) => (
              <IdentityPanelRow
                key={step.key}
                accent={step.met ? undefined : "warning"}
                title={step.title}
                detail={step.detail}
                trailing={step.met ? "Yes" : "No"}
              />
            ))
          ) : (
            <IdentityPanelRow
              title={`${metCount} of ${statusSteps.length} met`}
              detail={statusSteps.map((step) => step.title).join(", ")}
            />
          )}
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
                  accent={
                    coverage?.flag ||
                    device.policyState === "outdated" ||
                    device.policyState === "halted"
                      ? "warning"
                      : undefined
                  }
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
