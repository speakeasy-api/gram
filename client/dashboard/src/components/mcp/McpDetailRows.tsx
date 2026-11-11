import { DetailSidebarInfoLabel } from "@/components/detail/detail-sidebar-nav";
import { CopyButton } from "@/components/ui/CopyButton";
import { Text } from "@/components/ui/Text";
import {
  MCPServerAvailabilityToggle,
  MCPServerStatusDropdown,
} from "@/pages/mcp/x/MCPServerDetails";
import { type IdentityMode } from "@/lib/remote-identity";
import { IdentityExplainerDialog } from "@/lib/remote-identity";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { Info } from "lucide-react";
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/HoverCard";
import { cn } from "@/lib/utils";
import {
  SIGN_IN_DETECTED_STATUS,
  UNKNOWN_STATUS,
  UPSTREAM_AUTH_STATUS,
} from "@/lib/auth-clients";
import { enumPresentation } from "@/lib/enum";
import * as React from "react";
import { Link } from "react-router";

// The dot colours carry the state at a glance, matching the identity symbols
// AIM-230 settled on: blue for per-user, green for the shared agent, grey for
// none, amber when none is configured but the upstream demands a credential.
const IDENTITY_DOT: Record<IdentityMode, string> = {
  user: "bg-information-default",
  agent: "bg-success-default",
  none: "bg-muted-foreground/50",
};

/**
 * The Identity summary for an organization that enforces upstream auth. It
 * reports the server's upstream sign-in status from its credentials, the
 * same presentation the Identity settings section uses.
 */
export function UpstreamCredentialsIdentitySummary({
  status,
  authenticationRequired,
  unavailable,
  loading,
  settingsHref,
}: {
  /** The server's upstream auth status; undefined while unknown. */
  status: string | undefined;
  authenticationRequired: boolean;
  unavailable: boolean;
  loading: boolean;
  settingsHref: string;
}): React.JSX.Element {
  let value: React.JSX.Element;
  if (unavailable || (!loading && status === undefined)) {
    value = (
      <Text small className="text-destructive">
        Identity unavailable
      </Text>
    );
  } else if (loading || status === undefined) {
    value = (
      <Text muted small>
        Loading…
      </Text>
    );
  } else {
    const presentation =
      status === "no_auth_required" && authenticationRequired
        ? SIGN_IN_DETECTED_STATUS
        : enumPresentation(UPSTREAM_AUTH_STATUS, status, UNKNOWN_STATUS);
    const warn = presentation.tone === "warning";
    const pill = (
      <Link
        to={settingsHref}
        aria-label={`Identity: ${presentation.label}. Open identity settings`}
        className={cn(
          "bg-card hover:border-input hover:bg-muted/40 flex w-fit shrink-0 items-center gap-2 border px-2.5 py-1.5 text-sm font-medium transition-colors",
          warn && "border-warning-default",
        )}
      >
        <span
          aria-hidden="true"
          className={cn(
            "size-2 shrink-0 rounded-full",
            UPSTREAM_STATUS_DOT[presentation.tone],
          )}
        />
        {presentation.label}
      </Link>
    );
    value = warn ? (
      <HoverCard openDelay={150}>
        <HoverCardTrigger asChild>{pill}</HoverCardTrigger>
        <HoverCardContent align="start" className="w-72">
          <Text small className="block">
            {presentation.hint}
          </Text>
        </HoverCardContent>
      </HoverCard>
    ) : (
      pill
    );
  }

  return (
    <div className="flex items-center justify-between gap-2">
      <DetailSidebarInfoLabel>Identity</DetailSidebarInfoLabel>
      {value}
    </div>
  );
}

const UPSTREAM_STATUS_DOT: Record<"success" | "warning" | "neutral", string> = {
  success: "bg-success-default",
  warning: "bg-warning-default",
  neutral: "bg-muted-foreground/50",
};

function remoteIdentityLabel(mode: IdentityMode): string {
  switch (mode) {
    case "user":
      return "User";
    case "agent":
      return "Shared credential";
    case "none":
      return "Manual";
  }
}

export function RemoteIdentitySummary({
  mode,
  passThroughAuthorization,
  authenticationRequired,
  unavailable,
  loading,
  settingsHref,
}: {
  mode: IdentityMode;
  passThroughAuthorization: boolean;
  /** None is configured but the upstream answered the probe with a challenge. */
  authenticationRequired: boolean;
  unavailable: boolean;
  loading: boolean;
  settingsHref: string;
}): React.JSX.Element {
  const [explainerOpen, setExplainerOpen] = React.useState(false);
  const warn = passThroughAuthorization || authenticationRequired;
  const label = passThroughAuthorization
    ? "Needs cleanup"
    : remoteIdentityLabel(mode);

  // Only the warning states have something to reveal; a healthy identity is
  // fully described by the pill itself.
  let problem: string | null = null;
  if (passThroughAuthorization) {
    problem =
      "A legacy pass-through Authorization header is still configured. Remove it under Custom Headers so this server's identity is the only thing sending a credential.";
  } else if (authenticationRequired) {
    problem =
      "This server answers with an authentication challenge, but no identity is configured. Requests will keep failing until User Identity or a shared credential is set up.";
  }

  let status: React.JSX.Element;
  if (unavailable) {
    status = (
      <Text small className="text-destructive">
        Identity unavailable
      </Text>
    );
  } else if (loading) {
    status = (
      <Text muted small>
        Loading…
      </Text>
    );
  } else {
    // The pill is the way into the setting it reports, so it is the link.
    const pill = (
      <Link
        to={settingsHref}
        aria-label={`Identity: ${label}. Open identity settings`}
        className={cn(
          "bg-card hover:border-input hover:bg-muted/40 flex w-fit shrink-0 items-center gap-2 border px-2.5 py-1.5 text-sm font-medium transition-colors",
          warn && "border-warning-default",
        )}
      >
        <span
          aria-hidden="true"
          className={cn(
            "size-2 shrink-0 rounded-full",
            warn ? "bg-warning-default" : IDENTITY_DOT[mode],
          )}
        />
        {label}
      </Link>
    );

    status = problem ? (
      <HoverCard openDelay={150}>
        <HoverCardTrigger asChild>{pill}</HoverCardTrigger>
        <HoverCardContent align="start" className="w-72">
          <Text small className="block">
            {problem}
          </Text>
        </HoverCardContent>
      </HoverCard>
    ) : (
      pill
    );
  }

  return (
    // One row: what the setting is on the left, what it currently is on the
    // right. The pill is small enough that a second line spent nothing.
    <div className="flex items-center justify-between gap-2">
      <div className="flex min-w-0 items-center gap-1.5">
        <DetailSidebarInfoLabel>Identity</DetailSidebarInfoLabel>
        {/* The question the label raises — what these modes mean — rather than
            something about the value beside it. */}
        <button
          type="button"
          onClick={() => setExplainerOpen(true)}
          aria-label="What do these identity modes mean?"
          className="text-muted-foreground hover:text-foreground shrink-0"
        >
          <Info aria-hidden="true" className="size-3" />
        </button>
      </div>
      {status}
      <IdentityExplainerDialog
        open={explainerOpen}
        onOpenChange={setExplainerOpen}
      />
    </div>
  );
}

/**
 * A labelled URL in the rail. The value is long and the rail is narrow, so it
 * sits on one clipped line and the whole thing appears on hover.
 *
 * The reveal is a hover card rather than an absolutely-positioned child:
 * SidebarContent scrolls (overflow-auto), which clips any descendant that
 * tries to escape the rail no matter what z-index it carries. Radix portals
 * the content to the body, so it floats over the page as intended.
 */
export function SidebarUrlRow({
  label,
  url,
  copyTooltip,
}: {
  label: string;
  url: string;
  copyTooltip: string;
}): React.JSX.Element {
  const display = url.replace(/^https?:\/\//, "");
  return (
    <div className="flex flex-col gap-1">
      {/* The copy button sits beside the label, not the URL: the hover card
        that reveals the full URL would otherwise cover it. */}
      <div className="flex items-center gap-1">
        <DetailSidebarInfoLabel>{label}</DetailSidebarInfoLabel>
        <CopyButton text={url} size="xs" tooltip={copyTooltip} />
      </div>
      <div className="min-w-0">
        {/* No delay: this is a reveal of text already on screen, not a
          disclosure of extra information. */}
        <HoverCard openDelay={0}>
          <HoverCardTrigger asChild>
            {/* The padding belongs to the trigger, not the text inside it:
              the card aligns to the trigger's box, and matching insets are
              what land the two texts on each other. The negative margin
              cancels the indent so the URL stays flush with its label. */}
            <span className="-mx-2 block px-2 py-1">
              <Text
                variant="small"
                muted
                data-slot="sidebar-url-line"
                className="block truncate font-mono text-xs"
              >
                {display}
              </Text>
            </span>
          </HoverCardTrigger>
          {/* The card carries the same insets as the line, so aligning their
            boxes aligns their text: pull up by exactly the trigger's height
            and the two land on each other. */}
          <HoverCardContent
            align="start"
            side="bottom"
            sideOffset={-24}
            data-slot="sidebar-url-full"
            className="w-auto max-w-none px-2 py-1 font-mono text-xs whitespace-nowrap duration-75"
          >
            {display}
          </HoverCardContent>
        </HoverCard>
      </div>
    </div>
  );
}

export function McpServerCardStatus({
  server,
}: {
  server: McpServer;
}): React.JSX.Element | null {
  if (server.remoteMcpServerId) {
    return <MCPServerAvailabilityToggle server={server} />;
  }

  // Clients connect straight to the vendor, so visibility has nothing to gate.
  if (server.unproxiedMcpServerId) {
    return null;
  }

  return (
    <div className="flex flex-col gap-1.5">
      <DetailSidebarInfoLabel>Visibility</DetailSidebarInfoLabel>
      <MCPServerStatusDropdown server={server} />
    </div>
  );
}
