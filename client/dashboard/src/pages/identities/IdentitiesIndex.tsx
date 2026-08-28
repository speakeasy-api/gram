import {
  buildEmployees,
  type Employee,
} from "@/components/observe/insightsEmployeesData";
import { StatTile, StatTileGroup } from "@/components/chart/stat-tile";
import { defineFilters, useFilterState } from "@/components/filters";
import { useOrganization } from "@/contexts/Auth";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import { Badge } from "@/components/ui/Badge";
import { Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { getInitials } from "@/lib/initials";
import { encodeIdentityUrn } from "@/lib/identity-urn";
import { useOrgRoutes } from "@/routes";
import { telemetrySearchUsers } from "@gram/client/funcs/telemetrySearchUsers";
import { Source } from "@gram/client/models/components/searchuserspayload.js";
import type { UserSummary } from "@gram/client/models/components/usersummary.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { unwrapAsync } from "@gram/client/types/fp";
import { useQuery } from "@tanstack/react-query";
import { Bot, CircleHelp } from "lucide-react";
import { useMemo, useState } from "react";
import { Navigate, Outlet, useNavigate } from "react-router";
import {
  identityKindOf,
  identityUrnForEmployee,
  IDENTITY_KIND_LABELS,
  type IdentityKind,
} from "./identityKind";

export function IdentitiesRoot(): JSX.Element {
  return <Outlet />;
}

/**
 * The project-level Employee Enrollment index moved to the org-level Identities
 * list. Existing links land here and are sent on.
 */
export function IdentitiesIndexRedirect(): JSX.Element {
  const orgRoutes = useOrgRoutes();
  return <Navigate to={orgRoutes.identities.href()} replace />;
}

// The roster reaches back further than any org has existed, so every identity
// the telemetry knows about is listed however long ago it was last seen.
const ALL_TIME_FROM = new Date("2020-01-01T00:00:00Z");

const IDENTITY_FILTERS = defineFilters([
  { id: "kind", label: "Kind", kind: "multiselect", pinned: true },
]);

const KIND_OPTIONS = (["person", "unknown", "agent"] as IdentityKind[]).map(
  (kind) => ({ value: kind, label: IDENTITY_KIND_LABELS[kind] }),
);

export default function IdentitiesIndex(): JSX.Element {
  return (
    <RequireScope scope={["org:read", "org:admin"]} level="page">
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <IdentitiesIndexContent />
        </Page.Body>
      </Page>
    </RequireScope>
  );
}

function IdentitiesIndexContent(): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const organization = useOrganization();
  const navigate = useNavigate();
  const client = useGramContext();
  const [search, setSearch] = useState("");
  const { values, setValue, clearValue, clearAll } =
    useFilterState(IDENTITY_FILTERS);

  const membersQuery = useMembers(undefined, undefined, {
    throwOnError: false,
  });
  const rolesQuery = useRoles(undefined, undefined, { throwOnError: false });
  // Usage is the only project-scoped read here, and it is what surfaces the
  // identities the directory has never heard of.
  //
  // Unwindowed on purpose: this is the roster, not a report. Someone who has
  // been quiet for a quarter is still an identity, and hiding them behind a
  // date range would make the list answer "who was active lately" — a question
  // the per-identity pages already answer, each over its own window.
  const usageQuery = useQuery({
    queryKey: ["identities", "usage", "all-time", organization.id],
    queryFn: () => fetchIdentityUsage(client, ALL_TIME_FROM, new Date()),
    throwOnError: false,
  });

  const identities = useMemo(
    () =>
      buildEmployees(
        membersQuery.data?.members ?? [],
        rolesQuery.data?.roles ?? [],
        usageQuery.data ?? [],
      ),
    [membersQuery.data, rolesQuery.data, usageQuery.data],
  );

  const counts = useMemo(() => {
    const tally = { enrolled: 0, unknown: 0, agent: 0 };
    for (const identity of identities) {
      if (identity.status === "enrolled") tally.enrolled += 1;
      const kind = identityKindOf(identity);
      if (kind === "unknown") tally.unknown += 1;
      if (kind === "agent") tally.agent += 1;
    }
    return tally;
  }, [identities]);

  // Joined rather than held as an array: the filter state hands back a fresh
  // array each render, which would defeat the memo below.
  const kindKey = (values.kind ?? []).join(",");
  const rows = useMemo(() => {
    const selectedKinds = kindKey ? kindKey.split(",") : [];
    const query = search.trim().toLowerCase();
    return identities.filter((identity) => {
      if (
        selectedKinds.length > 0 &&
        !selectedKinds.includes(identityKindOf(identity))
      ) {
        return false;
      }
      if (!query) return true;
      return (
        identity.name.toLowerCase().includes(query) ||
        identity.email.toLowerCase().includes(query)
      );
    });
  }, [identities, search, kindKey]);

  const columns: Column<Employee>[] = [
    {
      key: "identity",
      header: "Identity",
      width: "1.6fr",
      render: (identity) => <IdentityCell identity={identity} />,
    },
    {
      key: "kind",
      header: "Kind",
      width: "140px",
      render: (identity) => {
        const kind = identityKindOf(identity);
        return (
          <Badge variant={kind === "person" ? "neutral" : "information"}>
            {IDENTITY_KIND_LABELS[kind]}
          </Badge>
        );
      },
    },
    {
      key: "status",
      header: "Enrollment",
      width: "150px",
      render: (identity) => (
        <Text muted small className="truncate">
          {identity.status === "enrolled" ? "Enrolled" : "Not enrolled"}
        </Text>
      ),
    },
    {
      key: "role",
      header: "Roles",
      width: "1fr",
      render: (identity) => (
        <Text muted small className="truncate">
          {identityKindOf(identity) === "person" ? identity.role : "—"}
        </Text>
      ),
    },
    {
      key: "lastActivity",
      header: "Last activity",
      width: "200px",
      render: (identity) => (
        <Text muted small className="truncate">
          {identity.lastActivity}
        </Text>
      ),
    },
    {
      key: "tokens",
      header: "Tokens",
      width: "120px",
      render: (identity) => (
        <Text small className="tabular-nums">
          {identity.tokenCount.toLocaleString()}
        </Text>
      ),
    },
  ];

  return (
    <Page.Section>
      <Page.Section.Title>Identities</Page.Section.Title>
      <Page.Section.Description>
        {rows.length} of {identities.length} — directory members, the addresses
        behind unattributed activity, and the agents that reported for
        themselves.
      </Page.Section.Description>
      <Page.Section.Body>
        <StatTileGroup>
          <StatTile
            title="Identities"
            value={identities.length}
            format="compact"
            tone="neutral"
            icon="users"
          />
          <StatTile
            title="Enrolled"
            value={counts.enrolled}
            format="compact"
            tone="success"
            icon="circle-check"
          />
          <StatTile
            title="Unattributed"
            value={counts.unknown}
            format="compact"
            tone={counts.unknown > 0 ? "warning" : "neutral"}
            icon="circle-help"
          />
          <StatTile
            title="Agents"
            value={counts.agent}
            format="compact"
            tone="information"
            icon="bot"
          />
        </StatTileGroup>
        <Page.Toolbar>
          <Page.Toolbar.Search
            value={search}
            onChange={setSearch}
            placeholder="Search identities…"
            debounceMs={200}
          />
          <Page.Toolbar.Filters
            schema={IDENTITY_FILTERS}
            values={values}
            optionsById={{ kind: KIND_OPTIONS }}
            onChange={setValue as (id: string, value: unknown) => void}
            onClear={clearValue as (id: string) => void}
            onClearAll={clearAll}
          />
        </Page.Toolbar>
        <Table
          columns={columns}
          data={rows}
          rowKey={(row) => row.id}
          onRowClick={(row) =>
            void navigate(
              orgRoutes.identities.detail.overview.href(
                encodeIdentityUrn(identityUrnForEmployee(row)),
              ),
            )
          }
          noResultsMessage="No identities match these filters"
        />
      </Page.Section.Body>
    </Page.Section>
  );
}

/**
 * The leading cell. The three kinds get different faces on purpose: a member
 * has a photo or initials, an unclaimed address gets a question mark, and an
 * agent id gets a bot — the row should say what sort of thing it is before the
 * reader gets to the Kind column.
 */
function IdentityCell({ identity }: { identity: Employee }): JSX.Element {
  const kind = identityKindOf(identity);
  const secondary = kind === "person" ? identity.email : identity.name;

  return (
    <div className="flex min-w-0 items-center gap-3">
      <Avatar className="size-8">
        {kind === "person" && identity.photoUrl && (
          <AvatarImage src={identity.photoUrl} alt={identity.name} />
        )}
        <AvatarFallback className="text-[11px] font-medium">
          {kind === "person" ? (
            getInitials(identity.name)
          ) : kind === "agent" ? (
            <Bot className="size-4" />
          ) : (
            <CircleHelp className="size-4" />
          )}
        </AvatarFallback>
      </Avatar>
      <div className="flex min-w-0 flex-col">
        <Text
          className={
            kind === "person"
              ? "truncate font-medium"
              : "truncate font-mono text-sm"
          }
        >
          {identity.name}
        </Text>
        {kind === "person" && secondary && (
          <Text muted small className="truncate text-xs">
            {secondary}
          </Text>
        )}
      </div>
    </div>
  );
}

async function fetchIdentityUsage(
  client: Parameters<typeof telemetrySearchUsers>[0],
  from: Date,
  to: Date,
): Promise<UserSummary[]> {
  const users: UserSummary[] = [];
  let cursor: string | undefined;

  do {
    const result = await unwrapAsync(
      telemetrySearchUsers(client, {
        searchUsersPayload: {
          cursor,
          filter: { from, to },
          limit: 1000,
          sort: "desc",
          userType: "internal",
          // The pre-aggregated agent-usage view: this list renders identity,
          // last activity and token totals, all of which it already holds.
          source: Source.AgentMetrics,
        },
      }),
    );

    users.push(...result.users);
    cursor = result.nextCursor;
  } while (cursor);

  return users;
}
