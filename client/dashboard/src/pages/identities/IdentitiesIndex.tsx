import {
  buildEmployees,
  type Employee,
  type EmployeeAccount,
} from "@/components/observe/insightsEmployeesData";
import { AccountRow } from "@/components/observe/account-display";
import { PERSONAL_ACCOUNT_GOVERNANCE_NOTE } from "@/lib/personal-account-governance";
import { Icon } from "@/components/ui/Icon";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { cn } from "@/lib/utils";
import { Info } from "lucide-react";
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
import { IdentityLink } from "@/components/identity-link";
import { getInitials } from "@/lib/initials";
import { encodeIdentityUrn } from "@/lib/identity-urn";
import { useRoutes } from "@/routes";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { useQuery } from "@tanstack/react-query";
import { Bot } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import {
  Navigate,
  Outlet,
  useLocation,
  useNavigate,
  useParams,
} from "react-router";
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
 * The bare detail URL has no content of its own — every panel lives on a tab —
 * so send it to the overview rather than render a header over an empty pane.
 * Hand-typed and truncated links land here.
 */
export function IdentityDetailIndexRedirect(): JSX.Element {
  const routes = useRoutes();
  const location = useLocation();
  const { identityUrn = "" } = useParams<{ identityUrn: string }>();
  return (
    <Navigate
      to={`${routes.identities.detail.overview.href(
        encodeIdentityUrn(identityUrn),
      )}${location.search}`}
      replace
    />
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
  {
    id: "account_type",
    label: "Account type",
    kind: "select",
    allLabel: "All",
    description: "Usage on personal accounts versus team-managed ones.",
  },
]);

const ACCOUNT_TYPE_OPTIONS = [
  { value: "personal", label: "Personal" },
  { value: "team", label: "Team" },
];

const KIND_OPTIONS = (["person", "agent"] as IdentityKind[]).map((kind) => ({
  value: kind,
  label: IDENTITY_KIND_LABELS[kind],
}));

// One em dash for every kind of "no role": an agent has none by definition, a
// person with no account has none yet, and the roster reports an absent role
// as a bare "-" or "Unknown" depending on which source answered. Three
// spellings of nothing in one column read as three different states.
function roleLabel(identity: Employee): string {
  if (identityKindOf(identity) !== "person") return "\u2014";
  const role = identity.role.trim();
  if (!role || role === "-" || role === "Unknown") return "\u2014";
  return role;
}

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
        {roleLabel(identity)}
      </Text>
    ),
  },
  {
    key: "accounts",
    header: (
      <span className="flex items-center gap-1">
        Accounts
        <SimpleTooltip
          tooltip={`The AI provider accounts (Claude, Codex, Cursor) each identity has been seen using, labelled team or personal. Accounts are linked automatically from tool activity, so this stays blank until an identity is seen using a recognized account. ${PERSONAL_ACCOUNT_GOVERNANCE_NOTE}`}
        >
          <Info className="text-muted-foreground size-3 shrink-0" />
        </SimpleTooltip>
      </span>
    ),
    width: "1fr",
    sortable: true,
    sortLabel: "Accounts",
    // Personal-holders first (ascending), then more accounts before fewer, so
    // the rows worth a second look group at the top.
    sortValue: (identity) =>
      (identity.hasPersonalAccount ? 0 : 1_000_000) - identity.accounts.length,
    render: (identity) => <AccountsCell identity={identity} />,
  },
  {
    key: "lastActivity",
    header: "Last activity",
    width: "200px",
    sortable: true,
    sortValue: (identity) => identity.lastActivityTimestamp ?? 0,
    render: (identity) => <LastActivityCell identity={identity} />,
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
    <RequireScope scope={["project:read"]} level="page">
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
  const routes = useRoutes();
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
      const kind = identityKindOf(identity);
      if (kind === "person" && !identityHasAccount(identity)) {
        tally.noAccount += 1;
      }
      if (kind === "agent") tally.agent += 1;
    }
    return tally;
  }, [identities]);

  // Joined rather than held as an array: the filter state hands back a fresh
  // array each render, which would defeat the memo below.
  const kindKey = (values.kind ?? []).join(",");
  const accountType = (values.account_type as string | undefined) ?? "";
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
      // Each value matches an identity holding at least one account of that
      // type; someone with both a team and a personal account shows under
      // either.
      if (
        accountType &&
        !identity.accounts.some((a) => a.accountType === accountType)
      ) {
        return false;
      }
      if (!query) return true;
      return (
        identity.name.toLowerCase().includes(query) ||
        identity.email.toLowerCase().includes(query)
      );
    });
  }, [identities, search, kindKey, accountType]);

  const sortedRows = useMemo(
    () => sortTableData(rows, IDENTITY_COLUMNS, sort) as Employee[],
    [rows, sort],
  );
  // Any change to what is being listed starts the list over: keeping a deep
  // scroll position across a new filter shows the reader page four of
  // something they have not seen page one of.
  useEffect(() => {
    setVisibleCount(PAGE_SIZE);
  }, [search, kindKey, accountType, sort]);

  return (
    <Page.Section>
      <Page.Section.Title>Identities</Page.Section.Title>
      <Page.Section.Description>
        {rows.length} of {identities.length} — every person and agent the
        platform knows about, account here or not.
      </Page.Section.Description>
      <Page.Section.Body>
        <StatTileGroup className="overflow-x-auto [&>*]:min-w-[11.5rem]">
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
            title="No linked account"
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
            optionsById={{
              kind: KIND_OPTIONS,
              account_type: ACCOUNT_TYPE_OPTIONS,
            }}
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
              routes.identities.detail.overview.href(
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
 * When the directory knows which account produced the most recent activity,
 * the timestamp names it — the workspace this identity was last working in.
 * Plain text otherwise.
 */
function LastActivityCell({ identity }: { identity: Employee }): JSX.Element {
  if (!identity.mostRecentAccount) {
    return (
      <Text muted small className="truncate">
        {identity.lastActivity}
      </Text>
    );
  }

  return (
    <AccountsPopover
      label={identity.lastActivity}
      labelClassName="text-xs"
      title="Most recent account"
      accounts={[identity.mostRecentAccount]}
    />
  );
}

/**
 * The linked accounts behind one row: a count that opens the list, because the
 * addresses themselves are too long to sit in a column and the question a
 * reader has here is "how many, and are any personal".
 */
function AccountsCell({ identity }: { identity: Employee }): JSX.Element {
  const { accounts } = identity;
  if (accounts.length === 0) {
    return <span className="text-muted-foreground/50 text-sm">&mdash;</span>;
  }

  return (
    <AccountsPopover
      label={`${accounts.length} account${accounts.length === 1 ? "" : "s"}`}
      labelClassName="text-xs"
      title="Linked accounts"
      accounts={accounts}
    />
  );
}

/**
 * The popover shell both account cells use: a trigger that says how many, and
 * a list naming each one with its provider and team/personal label.
 */
function AccountsPopover({
  label,
  labelClassName,
  title,
  accounts,
}: {
  label: string;
  labelClassName?: string;
  title: string;
  accounts: EmployeeAccount[];
}): JSX.Element {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          // The row navigates on click; opening the popover is not that.
          onClick={(event) => event.stopPropagation()}
          className="hover:bg-muted/60 -mx-1.5 flex items-center gap-1.5 px-1.5 py-1 transition-colors"
        >
          <span className={cn("text-muted-foreground", labelClassName)}>
            {label}
          </span>
          <Icon
            name="chevron-down"
            className="text-muted-foreground/60 size-3"
          />
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-72 p-0">
        <div className="border-b px-3 py-2">
          <p className="text-xs font-medium">{title}</p>
        </div>
        <ul className="divide-border/60 max-h-64 divide-y overflow-y-auto">
          {accounts.map((account, index) => (
            <li
              key={`${account.provider}:${account.email}:${index}`}
              className="px-3 py-2"
            >
              <AccountRow account={account} />
            </li>
          ))}
        </ul>
      </PopoverContent>
    </Popover>
  );
}

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

/**
 * The leading cell. Every person reads as a person — photo or initials, name in
 * the same weight — and only an agent gets a different face, because a bare id
 * it chose for itself may name no one. Whether they hold a linked account is
 * the Accounts column's job, which says it once and quietly.
 */
function IdentityCell({ identity }: { identity: Employee }): JSX.Element {
  const isAgent = identityKindOf(identity) === "agent";
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
          {/* The row click already navigates here, but a handler is not a
              link: no cmd+click, no middle-click, no copy-link, and nothing
              for a screen reader to announce. On a page whose whole job is
              reaching people, the name itself has to be the anchor. */}
          <Text className="truncate font-medium">
            <IdentityLink
              identifier={{ urn: identityUrnForEmployee(identity) }}
            >
              {identity.name}
            </IdentityLink>
          </Text>
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
