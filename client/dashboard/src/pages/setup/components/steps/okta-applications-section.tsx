import { useDeferredValue, useMemo, useState } from "react";
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
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { StepSection } from "../step-section";
import { errorMessage } from "./identity-provider-errors";
import { signOnModeLabel } from "./okta-sign-on-modes";

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

/** Okta reports status in upper case; the filter reads in lower. */
function isActive(application: IdentityProviderApplication): boolean {
  return (application.providerStatus ?? "").toLowerCase() === "active";
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
 * Assignment counts are read per application and can be absent: the server
 * omits them on a large tenant and drops the ones whose read failed, so a row
 * with no counts is saying it does not know, not that there are none.
 */
function assignedTo(application: IdentityProviderApplication): string {
  const parts: string[] = [];
  if (application.groupAssignmentCount !== undefined) {
    const count = application.groupAssignmentCount;
    parts.push(`${count} ${count === 1 ? "group" : "groups"}`);
  }
  if (application.userAssignmentCount !== undefined) {
    const count = application.userAssignmentCount;
    parts.push(`${count} ${count === 1 ? "person" : "people"}`);
  }
  return parts.length > 0 ? parts.join(", ") : "—";
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

// Okta does not hand us a logo with the inventory, so every card wears the
// same deterministic tile its name earns. Swap this for the image the moment
// the read carries one.
function ApplicationMark({ label }: { label: string }): JSX.Element {
  const tint = useIdentityTint(label);

  return (
    <div
      aria-hidden="true"
      className="flex h-9 w-9 flex-shrink-0 items-center justify-center text-xs font-semibold"
      style={tint}
    >
      {initials(label)}
    </div>
  );
}

function ApplicationCard({
  application,
  tenantHost,
}: {
  application: IdentityProviderApplication;
  tenantHost: string;
}): JSX.Element {
  const host = signOnHost(application);
  const distinct = host && host.toLowerCase() !== tenantHost;

  return (
    <div className="border-border bg-card flex flex-col gap-3 border p-4">
      <div className="flex items-start gap-3">
        <ApplicationMark label={application.label} />
        <div className="min-w-0 flex-1">
          <Text className="font-medium break-words">{application.label}</Text>
          {distinct ? (
            <Text variant="small" muted className="break-all">
              {host}
            </Text>
          ) : null}
        </div>
        {application.providerStatus ? (
          <Badge
            variant={isActive(application) ? "success" : "neutral"}
            background
            size="sm"
          >
            <Badge.Text>{application.providerStatus}</Badge.Text>
          </Badge>
        ) : null}
      </div>

      <div className="border-border flex flex-wrap items-baseline gap-x-4 gap-y-1 border-t pt-3">
        <Text variant="small" muted>
          {signOnModeLabel(application.signOnMode)}
        </Text>
        <Text variant="small" className="ml-auto">
          {assignedTo(application)}
        </Text>
      </div>
    </div>
  );
}

function readAtLabel(readAt: Date): string {
  return readAt.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

// Step four: what Okta has, read live and shown as it is. Nothing is stored and
// nothing is proposed — this is the inventory the later access work will be
// built on, shown now so the shape of the tenant is visible.
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
  // Search is not a filter dimension: it narrows a list read for this step and
  // has no meaning once the reader leaves it, so it does not belong in the URL.
  const [search, setSearch] = useState("");
  const tenantHost = tenantHostOf(connection?.tenantIdentifier ?? "");

  // The tenant list is read whole, so filtering is local; deferring keeps the
  // box responsive on a tenant with hundreds of applications.
  const deferredSearch = useDeferredValue(search);
  const rows = useMemo(() => {
    const all = applications.data?.applications ?? [];
    const needle = deferredSearch.trim().toLowerCase();
    return all.filter((application) => {
      const matchesSearch =
        needle.length === 0 || application.label.toLowerCase().includes(needle);
      const matchesStatus =
        !status ||
        (status === "active" ? isActive(application) : !isActive(application));
      return matchesSearch && matchesStatus;
    });
  }, [applications.data, deferredSearch, status]);

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
      <div className="space-y-4">
        <div>
          <Text variant="small" muted>
            {result.applicationCount}{" "}
            {result.applicationCount === 1 ? "application" : "applications"}{" "}
            read from Okta at {readAtLabel(result.readAt)}
          </Text>
          {/* The server says why counts are missing or capped; a dash in a row
              means nothing without it. */}
          {result.detail ? (
            <Text variant="small" muted>
              {result.detail}
            </Text>
          ) : null}
        </div>

        <Page.Toolbar>
          <Page.Toolbar.Search
            value={search}
            onChange={setSearch}
            placeholder="Search applications"
            debounceMs={200}
          />
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
              {rows.length} of {result.applications.length}
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
            heading="No applications match those filters"
            description="Clear the search or the status filter to see the rest of the tenant."
          />
        ) : (
          <div className="grid gap-3 sm:grid-cols-2">
            {rows.map((application) => (
              <ApplicationCard
                key={application.sourceApplicationId}
                application={application}
                tenantHost={tenantHost}
              />
            ))}
          </div>
        )}
      </div>
    );
  }

  return (
    <StepSection
      index={index}
      slug="applications"
      title="Applications and access"
      description="What Okta has, read live. Nothing here is stored or changed."
      locked={!active}
      badge={active ? undefined : "Waiting"}
    >
      {body}
    </StepSection>
  );
}
