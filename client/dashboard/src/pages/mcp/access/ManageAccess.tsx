import { AccessListRow, InlineChoice } from "@/components/access/AccessListRow";
import { IdentityLink } from "@/components/identity-link";
import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { Heading } from "@/components/ui/Heading";
import { SearchBar } from "@/components/ui/SearchBar";
import { SkeletonTable } from "@/components/ui/Skeleton";
import {
  PageTabsList,
  PageTabsTrigger,
  Tabs,
  TabsContent,
} from "@/components/ui/Tabs";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { useOrgRoutes } from "@/routes";
import type { ResourceAudienceEntry } from "@gram/client/models/components/resourceaudienceentry.js";
import type { SetResourceAudienceEntry } from "@gram/client/models/components/setresourceaudienceentry.js";
import { invalidateAllResourceAudience } from "@gram/client/react-query/resourceAudience.js";
import { useSetResourceAudienceMutation } from "@gram/client/react-query/setResourceAudience.js";
import { useQueryClient } from "@tanstack/react-query";
import { ChevronDown, ChevronLeft, ChevronRight } from "lucide-react";
import { useMemo, useState, type JSX } from "react";
import { toast } from "sonner";
import { AddAudienceDialog } from "./AddAudienceDialog";
import {
  ACCESS_PAGE_SIZE,
  AUDIENCE_LEVEL_FILTERS,
  AUDIENCE_TYPE_FILTERS,
  EMPTY_FILTERS,
  filterAudience,
  pageCount,
  pageOf,
  withAdded,
  withLevel,
  withoutPrincipals,
  type AudienceFilters,
} from "./manageAccessState";
import {
  audienceIcon,
  GRANTABLE_LEVELS,
  LEVEL_DESCRIPTION,
  LEVEL_LABEL,
  inheritedRules,
  ownRules,
  type AudienceLevel,
} from "./serverAudience";

type AddTarget = "people" | "groups" | null;

/**
 * Manage access for one resource: the rules naming it, editable in place, and
 * the organization-wide rules it inherits. Direct rules are the only ones this
 * surface writes, so the two tabs mirror the two places a rule can be owned.
 */
export function ManageAccess({
  resourceId,
  resourceName,
  entries,
  isLoading,
}: {
  resourceId: string;
  resourceName?: string;
  entries: ResourceAudienceEntry[];
  isLoading: boolean;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const queryClient = useQueryClient();

  const [tab, setTab] = useState("direct");
  const [filters, setFilters] = useState<AudienceFilters>(EMPTY_FILTERS);
  const [page, setPage] = useState(0);
  const [selected, setSelected] = useState<Set<string>>(() => new Set());
  const [adding, setAdding] = useState<AddTarget>(null);

  const direct = useMemo(() => ownRules(entries), [entries]);
  const inherited = useMemo(() => inheritedRules(entries), [entries]);
  const rows = tab === "direct" ? direct : inherited;

  const filtered = useMemo(
    () => filterAudience(rows, filters),
    [rows, filters],
  );
  const visible = pageOf(filtered, page);
  const pages = pageCount(filtered.length);

  const setAudience = useSetResourceAudienceMutation({
    onSuccess: async () => {
      await invalidateAllResourceAudience(queryClient);
      setSelected(new Set());
      setAdding(null);
    },
    onError: () => toast.error("Could not save access. Please try again."),
  });

  const save = (next: SetResourceAudienceEntry[], message: string) => {
    setAudience.mutate(
      {
        request: {
          setResourceAudienceForm: {
            resourceKind: "mcp",
            resourceId,
            entries: next,
          },
        },
      },
      { onSuccess: () => toast.success(message) },
    );
  };

  const changeLevel = (entry: ResourceAudienceEntry, level: AudienceLevel) => {
    // Changing an inherited rule writes a direct one for the same principal:
    // this surface only ever owns rules naming the resource, so the
    // organization-wide rule it inherits is left exactly as it was.
    save(
      withLevel(direct, entry.principalUrn, level),
      `${entry.displayName}: ${LEVEL_LABEL[level].toLowerCase()}.`,
    );
  };

  const removePrincipals = (principalUrns: string[]) => {
    const names = principalUrns.length === 1 ? "the rule" : "the rules";
    save(withoutPrincipals(direct, principalUrns), `Removed ${names}.`);
  };

  const addPrincipals = (principalUrns: string[]) => {
    save(
      withAdded(direct, principalUrns),
      principalUrns.length === 1
        ? "Access granted."
        : `${principalUrns.length} added.`,
    );
  };

  const toggleRow = (principalUrn: string) => {
    setSelected((current) => {
      const next = new Set(current);
      if (next.has(principalUrn)) next.delete(principalUrn);
      else next.add(principalUrn);
      return next;
    });
  };

  const allVisibleSelected =
    visible.length > 0 &&
    visible.every((row) => selected.has(row.principalUrn));

  if (isLoading) return <SkeletonTable />;

  return (
    <div>
      <div className="mb-4 flex items-center justify-between gap-4">
        <Heading variant="h4">Manage access</Heading>
        <div className="flex items-center gap-2">
          <orgRoutes.access.roles.Link>
            <Button variant="tertiary" size="sm">
              <Button.Text>Create role</Button.Text>
            </Button>
          </orgRoutes.access.roles.Link>
          <RequireScope
            scope="org:admin"
            level="component"
            reason="Only organization admins can change access."
          >
            <Button
              variant="secondary"
              size="sm"
              onClick={() => setAdding("people")}
            >
              <Button.Text>Add people</Button.Text>
            </Button>
          </RequireScope>
          <RequireScope
            scope="org:admin"
            level="component"
            reason="Only organization admins can change access."
          >
            <Button
              variant="secondary"
              size="sm"
              onClick={() => setAdding("groups")}
            >
              <Button.Text>Add roles</Button.Text>
            </Button>
          </RequireScope>
        </div>
      </div>

      <div className="border-border border">
        <Tabs
          className="gap-0"
          value={tab}
          onValueChange={(next) => {
            setTab(next);
            setPage(0);
            setSelected(new Set());
          }}
        >
          <div className="border-border flex flex-wrap items-center justify-between gap-3 border-b px-4">
            <PageTabsList>
              <PageTabsTrigger value="direct">Direct access</PageTabsTrigger>
              <PageTabsTrigger value="organization">
                Organization access
              </PageTabsTrigger>
            </PageTabsList>

            <div className="flex items-center gap-2 py-2">
              <FilterMenu
                label="Type"
                options={AUDIENCE_TYPE_FILTERS}
                value={filters.type}
                onChange={(value) => {
                  setFilters((f) => ({ ...f, type: value }));
                  setPage(0);
                }}
              />
              <FilterMenu
                label="Access"
                options={AUDIENCE_LEVEL_FILTERS}
                value={filters.level}
                onChange={(value) => {
                  setFilters((f) => ({ ...f, level: value }));
                  setPage(0);
                }}
              />
              <div className="w-56">
                <SearchBar
                  value={filters.search}
                  onChange={(value) => {
                    setFilters((f) => ({ ...f, search: value }));
                    setPage(0);
                  }}
                  placeholder="Find a person or group"
                />
              </div>
            </div>
          </div>

          {/* The selection strip only exists while something is selected, so
              the list is not topped by a permanently empty control bar. */}
          {tab === "direct" && selected.size > 0 && (
            <div className="border-border flex items-center gap-3 border-b px-4 py-2">
              <Checkbox
                checked={allVisibleSelected}
                onCheckedChange={(checked) => {
                  setSelected(
                    checked === true
                      ? new Set(visible.map((row) => row.principalUrn))
                      : new Set(),
                  );
                }}
                aria-label="Select all on this page"
              />
              <Text variant="body" className="text-sm">
                {selected.size} selected
              </Text>
              <Button
                variant="destructive-secondary"
                size="sm"
                onClick={() => removePrincipals([...selected])}
                disabled={setAudience.isPending}
              >
                <Button.Text>Remove</Button.Text>
              </Button>
            </div>
          )}

          <TabsContent value={tab} forceMount>
            {visible.length === 0 ? (
              <div className="px-4 py-12 text-center">
                <Text muted small>
                  {rows.length === 0
                    ? tab === "direct"
                      ? `Nobody has been given access to ${resourceName ?? "this server"} yet.`
                      : "No organization-wide rules cover this server."
                    : "No rules match these filters."}
                </Text>
              </div>
            ) : (
              <div className="divide-border divide-y">
                {visible.map((entry) => (
                  <AccessRow
                    key={`${entry.appliesTo}-${entry.principalUrn}`}
                    entry={entry}
                    selectable={tab === "direct"}
                    selected={selected.has(entry.principalUrn)}
                    onToggle={() => toggleRow(entry.principalUrn)}
                    onChangeLevel={(level) => changeLevel(entry, level)}
                    onRemove={() => removePrincipals([entry.principalUrn])}
                    pending={setAudience.isPending}
                  />
                ))}
              </div>
            )}
          </TabsContent>
        </Tabs>
      </div>

      {pages > 1 && (
        <div className="mt-4 flex items-center justify-center gap-4">
          <Button
            variant="tertiary"
            size="sm"
            disabled={page === 0}
            onClick={() => setPage((p) => Math.max(0, p - 1))}
          >
            <Button.LeftIcon>
              <ChevronLeft className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>Previous</Button.Text>
          </Button>
          <Text muted small>
            {page + 1} of {pages}
          </Text>
          <Button
            variant="tertiary"
            size="sm"
            disabled={page >= pages - 1}
            onClick={() => setPage((p) => Math.min(pages - 1, p + 1))}
          >
            <Button.Text>Next</Button.Text>
            <Button.RightIcon>
              <ChevronRight className="h-4 w-4" />
            </Button.RightIcon>
          </Button>
        </div>
      )}

      {adding && (
        <AddAudienceDialog
          title={adding === "people" ? "Add people" : "Add roles"}
          description={
            adding === "people"
              ? `Give people access to ${resourceName ?? "this server"} only.`
              : `Give a role access to ${resourceName ?? "this server"} only.`
          }
          kinds={adding === "people" ? ["user"] : ["everyone", "role"]}
          alreadyAdded={direct.map((entry) => entry.principalUrn)}
          pending={setAudience.isPending}
          onAdd={addPrincipals}
          onClose={() => setAdding(null)}
        />
      )}
    </div>
  );
}

function AccessRow({
  entry,
  selectable,
  selected,
  onToggle,
  onChangeLevel,
  onRemove,
  pending,
}: {
  entry: ResourceAudienceEntry;
  selectable: boolean;
  selected: boolean;
  onToggle: () => void;
  onChangeLevel: (level: AudienceLevel) => void;
  onRemove: () => void;
  pending: boolean;
}): JSX.Element {
  const Icon = audienceIcon(entry.kind);
  const userId =
    entry.kind === "user" ? entry.principalUrn.replace(/^user:/, "") : null;
  const toolCount = entry.tools?.length ?? 0;
  const ownRule = entry.appliesTo === "resource";

  return (
    <div className="flex items-start gap-3">
      <Checkbox
        checked={selected}
        onCheckedChange={onToggle}
        disabled={!selectable}
        aria-label={`Select ${entry.displayName}`}
        className={cn("mt-6 ml-4", selectable ? "" : "invisible")}
      />
      <div className="min-w-0 flex-1">
        {/* Same row component the role editor uses, so a rule reads the same
            way on both surfaces. */}
        <AccessListRow
          icon={<Icon className="h-4 w-4" />}
          title={
            userId ? (
              <IdentityLink identifier={{ userId }}>
                {entry.displayName}
              </IdentityLink>
            ) : (
              entry.displayName
            )
          }
          description={[
            entry.description,
            toolCount > 0
              ? `${toolCount} tool${toolCount === 1 ? "" : "s"}`
              : null,
          ]
            .filter(Boolean)
            .join(" · ")}
          meta={
            !ownRule ? (
              <Badge variant="neutral">
                <Badge.Text>Every server</Badge.Text>
              </Badge>
            ) : undefined
          }
          onRemove={onRemove}
          removeLabel={`Remove ${entry.displayName}`}
          removeDisabled={!ownRule || pending}
          removeReason="This rule is set for every server on the Access page."
        >
          <RequireScope
            scope="org:admin"
            level="component"
            reason="Only organization admins can change access."
          >
            <InlineChoice
              lead="Access"
              value={LEVEL_LABEL[entry.level]}
              disabled={pending}
              options={[
                ...GRANTABLE_LEVELS.map((level) => ({
                  label: LEVEL_LABEL[level],
                  description: LEVEL_DESCRIPTION[level],
                  onSelect: () => onChangeLevel(level),
                })),
                {
                  label: LEVEL_LABEL.blocked,
                  description: LEVEL_DESCRIPTION.blocked,
                  onSelect: () => onChangeLevel("blocked"),
                  separatorBefore: true,
                },
              ]}
            />
          </RequireScope>
        </AccessListRow>
      </div>
    </div>
  );
}

function FilterMenu<T extends string>({
  label,
  options,
  value,
  onChange,
}: {
  label: string;
  options: readonly { value: T; label: string }[];
  value: T;
  onChange: (value: T) => void;
}): JSX.Element {
  const active = options.find((option) => option.value === value);
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="tertiary" size="sm">
          <Button.Text>
            {active && active.value !== options[0]?.value
              ? `${label}: ${active.label}`
              : label}
          </Button.Text>
          <Button.RightIcon>
            <ChevronDown className="h-4 w-4" />
          </Button.RightIcon>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {options.map((option) => (
          <DropdownMenuItem
            key={option.value}
            onClick={() => onChange(option.value)}
          >
            {option.label}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export { ACCESS_PAGE_SIZE };
