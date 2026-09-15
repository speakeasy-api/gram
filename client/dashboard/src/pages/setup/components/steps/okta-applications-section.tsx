import { useDeferredValue, useMemo, useState } from "react";
import type { IdentityProviderApplication } from "@gram/client/models/components/identityproviderapplication.js";
import type { IdentityProviderConnection } from "@gram/client/models/components/identityproviderconnection.js";
import { useListIdentityProviderApplications } from "@gram/client/react-query/listIdentityProviderApplications.js";
import {
  defineFilters,
  useFilterState,
  type FilterValue,
} from "@/components/filters";
import { Page } from "@/components/page-layout";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Column, Table } from "@/components/ui/Table";
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

function buildColumns(
  tenantHost: string,
): Column<IdentityProviderApplication>[] {
  return [
    {
      key: "label",
      header: "Application",
      width: "2.5fr",
      render: (application) => {
        const host = signOnHost(application);
        const distinct = host && host.toLowerCase() !== tenantHost;
        return (
          <div className="min-w-0">
            <Text className="truncate font-medium">{application.label}</Text>
            {distinct ? (
              <Text variant="small" muted className="truncate">
                {host}
              </Text>
            ) : null}
          </div>
        );
      },
    },
    {
      key: "status",
      header: "Status",
      width: "1fr",
      render: (application) =>
        application.providerStatus ? (
          <Badge
            variant={isActive(application) ? "success" : "neutral"}
            background
            size="sm"
          >
            <Badge.Text>{application.providerStatus}</Badge.Text>
          </Badge>
        ) : (
          <Text muted>—</Text>
        ),
    },
    {
      key: "signOnMode",
      header: "Sign-on",
      width: "1.2fr",
      render: (application) => (
        <Text muted className="break-words whitespace-normal">
          {signOnModeLabel(application.signOnMode)}
        </Text>
      ),
    },
    {
      key: "assigned",
      header: "Assigned to",
      width: "1.5fr",
      render: (application) => <Text>{assignedTo(application)}</Text>,
    },
  ];
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
  const columns = useMemo(() => buildColumns(tenantHost), [tenantHost]);

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

        <Table
          columns={columns}
          data={rows}
          rowKey={(application) => application.sourceApplicationId}
          noResultsMessage={<Text>No applications match those filters.</Text>}
        />
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
