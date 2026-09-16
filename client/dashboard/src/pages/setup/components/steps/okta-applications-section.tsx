import { useMemo, useState } from "react";
import { Check } from "lucide-react";
import { Link } from "react-router";
import type { IdentityProviderApplication } from "@gram/client/models/components/identityproviderapplication.js";
import type { IdentityProviderConnection } from "@gram/client/models/components/identityproviderconnection.js";
import { useListIdentityProviderApplications } from "@gram/client/react-query/listIdentityProviderApplications.js";
import {
  defineFilters,
  useFilterState,
  type FilterValue,
} from "@/components/filters";
import { useIdentityTint } from "@/components/gradient-colors";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { Page } from "@/components/page-layout";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { useOrgRoutes, useRoutes } from "@/routes";
import { StepSection } from "../step-section";
import { errorMessage } from "./identity-provider-errors";
import {
  useApplicationDrafts,
  type DraftOutcome,
} from "./okta-application-drafts";

const STATUS_FILTERS = defineFilters([
  {
    id: "status",
    label: "Status",
    kind: "select",
    pinned: true,
    allLabel: "All",
  },
]);

const STATUS_OPTIONS = [
  { label: "Active", value: "active" },
  { label: "Inactive", value: "inactive" },
];

/** Group names past this many collapse into a single "+n more". */
const GROUP_NAME_CAP = 4;

/** Okta reports status in upper case; the filter reads in lower. */
function isActive(application: IdentityProviderApplication): boolean {
  return (application.providerStatus ?? "").toLowerCase() === "active";
}

/** Why the server would not let an application be picked, in the reader's terms. */
const UNPICKABLE_REASON: Record<string, string> = {
  inactive: "Inactive in Okta",
  no_match: "No known MCP server",
};

/**
 * Whether the card can be picked, and — when it cannot — the one line that
 * says why, in the place the status badge used to sit. The server decides:
 * it is the only side that knows whether Speakeasy has an MCP server for the
 * application, and it only marks one pickable once it has the endpoint.
 */
type PickState = { pickable: true } | { pickable: false; reason: string };

function pickState(application: IdentityProviderApplication): PickState {
  if (application.pickable && application.match?.remoteUrl) {
    return { pickable: true };
  }
  return {
    pickable: false,
    reason:
      UNPICKABLE_REASON[application.unpickableReason ?? ""] ??
      "No known MCP server",
  };
}

/**
 * The host is the part of a sign-on URL worth reading at a glance — it is what
 * tells two similarly named applications apart.
 */
function signOnHost(application: IdentityProviderApplication): string | null {
  if (!application.signOnUrl) return null;
  try {
    return new URL(application.signOnUrl).host;
  } catch {
    return application.signOnUrl;
  }
}

/**
 * The groups Okta has assigned to an application, as the chips a card shows.
 * Names past the cap, and the ones Okta had beyond the ten the server reads,
 * collapse into one trailing chip. Where the names could not be resolved but
 * the count could, the count stands in for them — it is what is known.
 */
function groupChips(application: IdentityProviderApplication): string[] {
  const names = (application.assignedGroups ?? []).map((group) => group.name);
  const overflow = application.assignedGroupOverflow ?? 0;

  if (names.length === 0) {
    const count = application.groupAssignmentCount ?? 0;
    if (count === 0) return [];
    return [`${count} ${count === 1 ? "group" : "groups"}`];
  }

  const shown = names.slice(0, GROUP_NAME_CAP);
  const hidden = names.length - shown.length + overflow;
  return hidden > 0 ? [...shown, `+${hidden} more`] : shown;
}

/**
 * Assignment counts are read per application and can be absent: the server
 * omits them on a large tenant and drops the ones whose read failed, so a card
 * with no count is saying it does not know, not that there is nobody.
 */
function peopleAssigned(application: IdentityProviderApplication): string {
  const count = application.userAssignmentCount;
  if (count === undefined) return "—";
  return `${count} ${count === 1 ? "person" : "people"}`;
}

/**
 * Most applications on a tenant sign on at the tenant's own host, so printing
 * it under every name says nothing. It earns its line only where it differs.
 */
function tenantHostOf(tenantIdentifier: string): string {
  return tenantIdentifier
    .replace(/^https?:\/\//, "")
    .replace(/\/.*$/, "")
    .toLowerCase();
}

/** Two letters of the name, which is what a tenant's own applications have. */
function initials(label: string): string {
  const words = label.trim().split(/\s+/).filter(Boolean);
  if (words.length === 0) return "?";
  if (words.length === 1) return words[0]!.slice(0, 2).toUpperCase();
  return (words[0]![0]! + words[1]![0]!).toUpperCase();
}

/** The button label counts what pressing it would create. */
function createLabel(count: number): string {
  if (count === 0) return "Create MCP Servers";
  return `Create ${count} MCP ${count === 1 ? "Server" : "Servers"}`;
}

function ApplicationMark({
  label,
  logoUrl,
  dimmed,
}: {
  label: string;
  logoUrl?: string;
  dimmed: boolean;
}): JSX.Element {
  const tint = useIdentityTint(label);
  const [failedLogoUrl, setFailedLogoUrl] = useState<string>();
  const showLogo = logoUrl && logoUrl !== failedLogoUrl;

  return (
    <div
      aria-hidden="true"
      className={cn(
        "flex h-9 w-9 flex-shrink-0 items-center justify-center text-xs font-semibold",
        dimmed && "opacity-55",
      )}
      style={showLogo ? undefined : tint}
    >
      {showLogo ? (
        <img
          src={logoUrl}
          alt=""
          referrerPolicy="no-referrer"
          className="size-full object-contain"
          onError={() => setFailedLogoUrl(logoUrl)}
        />
      ) : (
        initials(label)
      )}
    </div>
  );
}

/** Drawn, not a control: the card itself is what takes the click. */
function SelectionMark({ selected }: { selected: boolean }): JSX.Element {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "mt-0.5 flex h-4.5 w-4.5 flex-shrink-0 items-center justify-center border",
        selected
          ? "border-primary bg-primary text-primary-foreground"
          : "border-input bg-card",
      )}
    >
      {selected ? <Check className="h-3 w-3" strokeWidth={3} /> : null}
    </span>
  );
}

const CARD_CLASS = "border-border bg-card flex flex-col gap-3 border p-4";

/** The lead-in before the link, by how far the draft and its sign-in got. */
function outcomeSentence(
  outcome: DraftOutcome & { status: "created" | "exists" },
): string {
  if (outcome.status === "exists") {
    return "Already an MCP server in this project. Finish setup on ";
  }
  switch (outcome.identity.status) {
    case "configured":
      return "Draft created, sign-in provider configured. See ";
    case "manual":
      return "Draft created, sign-in provider needs manual setup. See ";
    case "configuring":
    case "failed":
    case "none":
      return "Draft created. Finish setup on ";
  }
}

/** The provider's own page, where a sign-in nobody could finish is finished. */
function ProviderLink({ providerId }: { providerId: string }): JSX.Element {
  const orgRoutes = useOrgRoutes();

  return (
    <Link
      to={orgRoutes.remoteIdentityProviders.issuerDetail.href(providerId)}
      className="underline underline-offset-2"
    >
      its sign-in provider
    </Link>
  );
}

/**
 * What became of the draft, on the card that asked for it: one line, with the
 * way on to the server's own page where its setup is finished, and to the
 * sign-in provider when there is one to look at.
 *
 * Only ever the sentences and links below — never a field off the committed
 * client, and never an upstream's own words.
 */
function DraftOutcomeLine({
  outcome,
  projectSlug,
  onRetry,
  onRetryIdentity,
}: {
  outcome: DraftOutcome;
  projectSlug: string;
  onRetry: () => void;
  onRetryIdentity: () => void;
}): JSX.Element {
  const routes = useRoutes({ projectSlug });

  if (outcome.status === "creating") {
    return (
      <Text variant="small" muted>
        Creating…
      </Text>
    );
  }

  if (outcome.status === "failed") {
    return <FailureLine message={outcome.message} onRetry={onRetry} />;
  }

  if (
    outcome.status === "created" &&
    outcome.identity.status === "configuring"
  ) {
    return (
      <Text variant="small" muted>
        Draft created. Configuring its sign-in provider…
      </Text>
    );
  }

  // The server stands whatever became of its sign-in, so the failure sits
  // under the line that says so, and the retry runs only those steps again.
  if (outcome.status === "created" && outcome.identity.status === "failed") {
    return (
      <div className="space-y-2">
        <Text variant="small">
          Draft created. Finish setup on{" "}
          <routes.mcp.x.Link params={[outcome.mcpServerParam]}>
            its page
          </routes.mcp.x.Link>
          .
        </Text>
        <FailureLine
          message={outcome.identity.message}
          onRetry={onRetryIdentity}
        />
      </div>
    );
  }

  const providerId =
    outcome.status === "created" &&
    (outcome.identity.status === "configured" ||
      outcome.identity.status === "manual")
      ? outcome.identity.providerId
      : undefined;

  return (
    <Text variant="small">
      {outcomeSentence(outcome)}
      <routes.mcp.x.Link params={[outcome.mcpServerParam]}>
        its page
      </routes.mcp.x.Link>
      {providerId ? (
        <>
          {" or "}
          <ProviderLink providerId={providerId} />
        </>
      ) : null}
      .
    </Text>
  );
}

function FailureLine({
  message,
  onRetry,
}: {
  message: string;
  onRetry: () => void;
}): JSX.Element {
  return (
    <div className="flex items-start justify-between gap-2">
      <Text variant="small" destructive className="break-words">
        {message}
      </Text>
      <Button variant="tertiary" size="sm" onClick={onRetry}>
        Retry
      </Button>
    </div>
  );
}

function ApplicationCard({
  application,
  tenantHost,
  selected,
  onToggle,
  outcome,
  projectSlug,
  onRetry,
  onRetryIdentity,
}: {
  application: IdentityProviderApplication;
  tenantHost: string;
  selected: boolean;
  onToggle: () => void;
  outcome: DraftOutcome | undefined;
  projectSlug: string;
  onRetry: () => void;
  onRetryIdentity: () => void;
}): JSX.Element {
  const host = signOnHost(application);
  const distinct = host && host.toLowerCase() !== tenantHost;
  const state = pickState(application);
  const chips = groupChips(application);

  const contents = (
    <>
      <div className="flex items-start gap-3">
        <ApplicationMark
          label={application.label}
          logoUrl={application.logoUrl}
          dimmed={!state.pickable}
        />
        <div className="min-w-0 flex-1 text-left">
          <Text
            className={cn(
              "font-medium break-words",
              !state.pickable && "opacity-55",
            )}
          >
            {application.label}
          </Text>
          {distinct ? (
            <Text variant="small" muted className="break-all">
              {host}
            </Text>
          ) : null}
        </div>
        {state.pickable ? (
          <SelectionMark selected={selected} />
        ) : (
          <Text variant="small" muted className="text-right">
            {state.reason}
          </Text>
        )}
      </div>

      {chips.length > 0 ? (
        <div className="flex flex-wrap gap-1.5">
          {chips.map((chip) => (
            <Badge key={chip} variant="neutral" background size="sm">
              <Badge.Text>{chip}</Badge.Text>
            </Badge>
          ))}
        </div>
      ) : null}

      <div className="border-border border-t pt-3">
        <Text variant="small">{peopleAssigned(application)}</Text>
      </div>

      {outcome ? (
        <DraftOutcomeLine
          outcome={outcome}
          projectSlug={projectSlug}
          onRetry={onRetry}
          onRetryIdentity={onRetryIdentity}
        />
      ) : null}
    </>
  );

  // A card that has been acted on stops being a control: what it now carries is
  // the way on to the server, or the way to try again.
  if (!state.pickable || outcome) {
    return <div className={CARD_CLASS}>{contents}</div>;
  }

  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={selected}
      aria-label={application.label}
      onClick={onToggle}
      className={cn(
        CARD_CLASS,
        "focus-visible:ring-ring/40 text-left outline-none focus-visible:ring-1",
        selected && "border-primary",
      )}
    >
      {contents}
    </button>
  );
}

/**
 * The foot of the grid's own frame: it stays put while the applications scroll
 * past it, so the count is readable from wherever you stopped picking.
 */
function CreateBar({
  count,
  creating,
  onCreate,
}: {
  count: number;
  creating: boolean;
  onCreate: () => void;
}): JSX.Element {
  return (
    <div className="border-border bg-card flex flex-shrink-0 justify-center border-t p-3">
      <Button
        variant="primary"
        size="md"
        disabled={count === 0 || creating}
        onClick={onCreate}
      >
        {creating ? "Creating…" : createLabel(count)}
      </Button>
    </div>
  );
}

function readAtLabel(readAt: Date): string {
  return readAt.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

type ApplicationsResult = NonNullable<
  ReturnType<typeof useListIdentityProviderApplications>["data"]
>;

/**
 * The inventory once it is in hand: the applications to pick from in a frame
 * that scrolls on its own, what narrows them, and the bar on that frame's
 * bottom edge that creates a draft for each pick. It mounts only with a read
 * behind it, so nothing here asks the project for anything until there is
 * something to act on.
 */
function ApplicationsInventory({
  result,
  tenantHost,
  onRefresh,
  refreshing,
}: {
  result: ApplicationsResult;
  tenantHost: string;
  onRefresh: () => void;
  refreshing: boolean;
}): JSX.Element {
  const { values, setValue, clearValue, clearAll } =
    useFilterState(STATUS_FILTERS);
  const status = values.status;
  const drafts = useApplicationDrafts();

  // Everything Speakeasy can draft starts picked, and the reader takes away
  // what they do not want — so what is held is what was taken away, not what
  // is left. An application that arrives on a refresh is then picked like the
  // rest, and one that leaves the tenant takes nothing with it. Nothing here
  // is stored: it is this reading of the step and nothing more.
  const [deselected, setDeselected] = useState<ReadonlySet<string>>(new Set());
  const toggle = (sourceApplicationId: string) => {
    setDeselected((previous) => {
      const next = new Set(previous);
      if (!next.delete(sourceApplicationId)) next.add(sourceApplicationId);
      return next;
    });
  };

  const all = result.applications;
  // The server sends them pickable first; filtering narrows that order, it
  // does not re-cut it.
  const rows = useMemo(
    () =>
      all.filter(
        (application) =>
          !status ||
          (status === "active"
            ? isActive(application)
            : !isActive(application)),
      ),
    [all, status],
  );

  // What pressing the bar would create, counted across the whole tenant rather
  // than the filtered view: narrowing the list hides cards, it does not unpick
  // them. A card that already has its server is not counted, and not created
  // again.
  const pending = all.filter((application) => {
    const outcome = drafts.outcomes[application.sourceApplicationId];
    return (
      pickState(application).pickable &&
      !deselected.has(application.sourceApplicationId) &&
      outcome?.status !== "created" &&
      outcome?.status !== "exists"
    );
  });

  return (
    <div className="space-y-4">
      <div>
        <Text variant="small" muted>
          {result.applicationCount}{" "}
          {result.applicationCount === 1 ? "application" : "applications"} read
          from Okta at {readAtLabel(result.readAt)}
        </Text>
        {/* The server says why counts are missing or capped; a dash on a card
            means nothing without it. */}
        {result.detail ? (
          <Text variant="small" muted>
            {result.detail}
          </Text>
        ) : null}
      </div>

      <Page.Toolbar>
        <Page.Toolbar.Filters
          schema={STATUS_FILTERS}
          values={values}
          optionsById={{ status: STATUS_OPTIONS }}
          onChange={setValue as (id: string, value: FilterValue) => void}
          onClear={clearValue as (id: string) => void}
          onClearAll={clearAll}
        />
        <Page.Toolbar.Actions>
          <Text variant="small" muted>
            {rows.length} of {all.length}
          </Text>
        </Page.Toolbar.Actions>
        <Page.Toolbar.Refresh onRefresh={onRefresh} isRefreshing={refreshing} />
      </Page.Toolbar>

      {/* One frame around the grid: a tenant's worth of applications scrolls
          inside it rather than pushing the step's own footing off the page,
          and the bar sits on its bottom edge where the count stays readable
          from wherever you stopped picking. */}
      <div className="border-border flex max-h-[32rem] flex-col border">
        <div className="min-h-0 flex-1 overflow-y-auto p-3">
          {rows.length === 0 ? (
            <InlineEmptyState
              icon="search-x"
              heading="No applications match that filter"
              description="Clear the status filter to see the rest of the tenant."
            />
          ) : (
            <div className="grid gap-3 sm:grid-cols-2">
              {rows.map((application) => (
                <ApplicationCard
                  key={application.sourceApplicationId}
                  application={application}
                  tenantHost={tenantHost}
                  selected={!deselected.has(application.sourceApplicationId)}
                  onToggle={() => toggle(application.sourceApplicationId)}
                  outcome={drafts.outcomes[application.sourceApplicationId]}
                  projectSlug={drafts.projectSlug}
                  onRetry={() => void drafts.create([application])}
                  onRetryIdentity={() => {
                    const outcome =
                      drafts.outcomes[application.sourceApplicationId];
                    if (outcome?.status !== "created") return;
                    void drafts.configureIdentity(
                      application.sourceApplicationId,
                      outcome.pair,
                    );
                  }}
                />
              ))}
            </div>
          )}
        </div>

        <CreateBar
          count={pending.length}
          creating={drafts.creating}
          onCreate={() => void drafts.create(pending)}
        />
      </div>
    </div>
  );
}

// Step four: what Okta has, read live, as a grid of applications to pick from.
// A card is pickable where Speakeasy knows an MCP server for it, and every one
// of those starts picked; the bar at the foot of the grid acts on what is left.
export function OktaApplicationsSection({
  index,
  connection,
}: {
  index: number;
  connection: IdentityProviderConnection | undefined;
}): JSX.Element {
  const active = connection?.status === "active";
  const applications = useListIdentityProviderApplications(
    undefined,
    undefined,
    { enabled: active, throwOnError: false },
  );

  let body: JSX.Element | null = null;
  if (applications.isPending) {
    body = <SkeletonTable />;
  } else if (applications.error) {
    // The server's own line is the specific reason, but on its own it reads
    // like a log entry, so it sits under a sentence that says what failed.
    const reason = errorMessage(applications.error, "");
    body = (
      <Alert variant="warning" alignTop>
        <div>
          <AlertTitle>
            Speakeasy could not read applications from Okta
          </AlertTitle>
          <AlertDescription>
            The connection is live, so this is about the read itself. Try again,
            and check that the tenant still grants the application scopes.
            {reason ? (
              <span className="text-muted-foreground mt-1 block">{reason}</span>
            ) : null}
          </AlertDescription>
        </div>
      </Alert>
    );
  } else if (applications.data) {
    body = (
      <ApplicationsInventory
        result={applications.data}
        tenantHost={tenantHostOf(connection?.tenantIdentifier ?? "")}
        onRefresh={() => void applications.refetch()}
        refreshing={applications.isFetching}
      />
    );
  }

  return (
    <StepSection
      index={index}
      slug="applications"
      title="Applications and access"
      description="Pick the applications to stand up as MCP servers. Read live from Okta; nothing is created until you say so."
      locked={!active}
      badge={active ? undefined : "Waiting"}
    >
      {body}
    </StepSection>
  );
}
