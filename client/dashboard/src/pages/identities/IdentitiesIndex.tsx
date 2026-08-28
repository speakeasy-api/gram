import {
  buildEmployees,
  type Employee,
} from "@/components/observe/insightsEmployeesData";
import { StatTile, StatTileGroup } from "@/components/chart/stat-tile";
import { defineFilters, useFilterState } from "@/components/filters";
import { useOrganization } from "@/contexts/Auth";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import { Badge } from "@/components/ui/Badge";
import { Column, type SortDescriptor, Table } from "@/components/ui/Table";
import { sortTableData } from "@/components/ui/Table/sorting";
import { Text } from "@/components/ui/Text";
import { getInitials } from "@/lib/initials";
import { encodeIdentityUrn } from "@/lib/identity-urn";
import { useOrgRoutes } from "@/routes";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { useQuery } from "@tanstack/react-query";
import { Bot } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Navigate, Outlet, useLocation, useNavigate } from "react-router";
import {
  identityHasAccount,
  identityKindOf,
  identityUrnForEmployee,
  IDENTITY_KIND_LABELS,
  type IdentityKind,
} from "./identityKind";
import { fetchIdentityRoster, identityRosterQueryKey } from "./identityRoster";

export function IdentitiesRoot(): JSX.Element {
  return <Outlet />;
}

/**
 * The project-level Employee Enrollment index moved to the org-level Identities
 * list. Existing links land here and are sent on.
 */
export function IdentitiesIndexRedirect(): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const location = useLocation();
  // Carry the query string: a link from a dashboard widget picks the window,
  // and dropping it lands the reader on a different period than they chose.
  return (
    <Navigate to={`${orgRoutes.identities.href()}${location.search}`} replace />
  );
}

// The roster is one merged list held in memory, so it pages here rather than
// at either source.
const PAGE_SIZE = 50;

const IDENTITY_FILTERS = defineFilters([
  {
    id: "kind",
    label: "Kind",
    kind: "multiselect",
    pinned: true,
    description: "People and agents.",
  },
]);

const KIND_OPTIONS = (["person", "agent"] as IdentityKind[]).map((kind) => ({
  value: kind,
  label: IDENTITY_KIND_LABELS[kind],
}));

const IDENTITY_COLUMNS: Column<Employee>[] = [
  {
    key: "identity",
    header: "Identity",
    width: "1.6fr",
    sortable: true,
    sortValue: (identity) => identity.name.toLowerCase(),
    render: (identity) => <IdentityCell identity={identity} />,
  },
  {
    key: "kind",
    header: "Kind",
    width: "140px",
    sortable: true,
    sortValue: (identity) => IDENTITY_KIND_LABELS[identityKindOf(identity)],
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
    sortable: true,
    sortValue: (identity) => identity.status,
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
        {identityKindOf(identity) !== "person"
          ? "—"
          : identity.role === "Unknown"
            ? "None"
            : identity.role}
      </Text>
    ),
  },
  {
    key: "lastActivity",
    header: "Last activity",
    width: "200px",
    sortable: true,
    sortValue: (identity) => identity.lastActivityTimestamp ?? 0,
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
    sortable: true,
    sortValue: (identity) => identity.tokenCount,
    render: (identity) => (
      <Text small className="tabular-nums">
        {identity.tokenCount.toLocaleString()}
      </Text>
    ),
  },
];

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
  const projectSlug = useProjectSlugForRequests();
  const navigate = useNavigate();
  const client = useGramContext();
  const [search, setSearch] = useState("");
  const [sort, setSort] = useState<SortDescriptor | null>({
    id: "lastActivity",
    direction: "desc",
  });
  const [visibleCount, setVisibleCount] = useState(PAGE_SIZE);
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
    queryKey: identityRosterQueryKey(organization.id, projectSlug),
    queryFn: () => fetchIdentityRoster(client, projectSlug),
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
    const tally = { enrolled: 0, noAccount: 0, agent: 0 };
    for (const identity of identities) {
      if (identity.status === "enrolled") tally.enrolled += 1;
      if (!identityHasAccount(identity)) tally.noAccount += 1;
      if (identityKindOf(identity) === "agent") tally.agent += 1;
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

  const sortedRows = useMemo(
    () => sortTableData(rows, IDENTITY_COLUMNS, sort) as Employee[],
    [rows, sort],
  );
  // Any change to what is being listed starts the list over: keeping a deep
  // scroll position across a new filter shows the reader page four of
  // something they have not seen page one of.
  useEffect(() => {
    setVisibleCount(PAGE_SIZE);
  }, [search, kindKey, sort]);

  return (
    <Page.Section>
      <Page.Section.Title>Identities</Page.Section.Title>
      <Page.Section.Description>
        {rows.length} of {identities.length} — every person and agent the
        platform knows about, account here or not.
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
            title="No account"
            value={counts.noAccount}
            format="compact"
            tone={counts.noAccount > 0 ? "warning" : "neutral"}
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
          columns={IDENTITY_COLUMNS}
          data={sortedRows.slice(0, visibleCount)}
          sort={sort}
          onSortChange={setSort}
          hasMore={visibleCount < sortedRows.length}
          onLoadMore={async () => {
            setVisibleCount((count) => count + PAGE_SIZE);
          }}
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
/**
 * Initials for a person whose only name is an address: getInitials splits on
 * spaces, which yields a single letter for `ana.vidal@…`. Read the local part
 * instead so an address-only person still gets a real monogram.
 */
function personInitials(name: string): string {
  if (!name.includes("@")) return getInitials(name);
  const local = name.slice(0, name.indexOf("@"));
  return getInitials(local.replace(/[._-]+/g, " "));
}

function IdentityCell({ identity }: { identity: Employee }): JSX.Element {
  const isAgent = identityKindOf(identity) === "agent";
  const hasAccount = identityHasAccount(identity);
  // A person with no member row has only their address, which is already the
  // name; repeating it underneath would be noise.
  const secondary = identity.email === identity.name ? "" : identity.email;

  return (
    <div className="flex min-w-0 items-center gap-3">
      <Avatar className="size-8">
        {identity.photoUrl && (
          <AvatarImage src={identity.photoUrl} alt={identity.name} />
        )}
        <AvatarFallback className="text-[11px] font-medium">
          {isAgent ? <Bot className="size-4" /> : personInitials(identity.name)}
        </AvatarFallback>
      </Avatar>
      <div className="flex min-w-0 flex-col">
        <div className="flex min-w-0 items-center gap-2">
          <Text className="truncate font-medium">{identity.name}</Text>
          {!isAgent && !hasAccount && (
            <Badge variant="neutral">No account</Badge>
          )}
        </div>
        {secondary && (
          <Text muted small className="truncate text-xs">
            {secondary}
          </Text>
        )}
      </div>
    </div>
  );
}
