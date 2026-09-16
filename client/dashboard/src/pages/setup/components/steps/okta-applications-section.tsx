import { useMemo, useState } from "react";
import { Check } from "lucide-react";
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
import { StepSection } from "../step-section";
import { errorMessage } from "./identity-provider-errors";

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

/**
 * Whether the card can be picked, and — when it cannot — the one line that
 * says why, in the place the status badge used to sit.
 */
type PickState =
  | { pickable: true }
  | { pickable: false; reason: "Inactive in Okta" | "No known MCP server" };

/**
 * TODO(S-916): the server decides this. Once listApplications carries the
 * catalog match, read its `pickable` / `unpickableReason` off the application
 * and delete this — an application Okta has active can still have no MCP
 * server Speakeasy knows of, which only the server can tell.
 */
function pickState(application: IdentityProviderApplication): PickState {
  if (!isActive(application)) {
    return { pickable: false, reason: "Inactive in Okta" };
  }
  return { pickable: true };
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

function ApplicationCard({
  application,
  tenantHost,
  selected,
  onToggle,
}: {
  application: IdentityProviderApplication;
  tenantHost: string;
  selected: boolean;
  onToggle: () => void;
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
    </>
  );

  if (!state.pickable) {
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
 * The only thing selection adds to the page. It sits over the grid rather than
 * after it, so the count stays readable from wherever you stopped picking.
 */
function CreateBar({ count }: { count: number }): JSX.Element {
  return (
    <div className="pointer-events-none fixed inset-x-0 bottom-0 z-20 flex justify-center p-4 pb-[calc(1rem_+_env(safe-area-inset-bottom,0px))]">
      <div className="border-input bg-card pointer-events-auto border p-2">
        {/* TODO(S-916): creates the drafts once the server carries the
            catalog match each selected application would be created from. */}
        <Button variant="primary" size="md" disabled={count === 0}>
          {createLabel(count)}
        </Button>
      </div>
    </div>
  );
}

function readAtLabel(readAt: Date): string {
  return readAt.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

// Step four: what Okta has, read live, as a grid of applications to pick from.
// A card is pickable where Speakeasy knows an MCP server for it; picking is
// what the create bar at the foot of the page acts on.
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

  const { values, setValue, clearValue, clearAll } =
    useFilterState(STATUS_FILTERS);
  const status = values.status;
  const tenantHost = tenantHostOf(connection?.tenantIdentifier ?? "");

  // Selection is this reading of the step and nothing more: it is not stored,
  // and an application that leaves the tenant leaves the selection with it.
  const [selection, setSelection] = useState<ReadonlySet<string>>(new Set());
  const toggle = (sourceApplicationId: string) => {
    setSelection((previous) => {
      const next = new Set(previous);
      if (!next.delete(sourceApplicationId)) next.add(sourceApplicationId);
      return next;
    });
  };

  const all = useMemo(
    () => applications.data?.applications ?? [],
    [applications.data],
  );

  const rows = useMemo(() => {
    const matching = all.filter(
      (application) =>
        !status ||
        (status === "active" ? isActive(application) : !isActive(application)),
    );
    // TODO(S-916): the server returns them in this order once it knows which
    // ones it has a match for — drop the sort and render what it sends.
    return matching
      .map((application, position) => ({ application, position }))
      .sort((left, right) => {
        const byPickable =
          Number(pickState(right.application).pickable) -
          Number(pickState(left.application).pickable);
        return byPickable !== 0 ? byPickable : left.position - right.position;
      })
      .map((entry) => entry.application);
  }, [all, status]);

  // Counted across the whole tenant, not the filtered view: narrowing the list
  // hides cards, it does not unpick them.
  const selectedCount = all.filter(
    (application) =>
      pickState(application).pickable &&
      selection.has(application.sourceApplicationId),
  ).length;

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
    const result = applications.data;
    body = (
      // Padded past the create bar so it never covers the last row.
      <div className="space-y-4 pb-24">
        <div>
          <Text variant="small" muted>
            {result.applicationCount}{" "}
            {result.applicationCount === 1 ? "application" : "applications"}{" "}
            read from Okta at {readAtLabel(result.readAt)}
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
          <Page.Toolbar.Refresh
            onRefresh={() => void applications.refetch()}
            isRefreshing={applications.isFetching}
          />
        </Page.Toolbar>

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
                selected={selection.has(application.sourceApplicationId)}
                onToggle={() => toggle(application.sourceApplicationId)}
              />
            ))}
          </div>
        )}

        <CreateBar count={selectedCount} />
      </div>
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
