import { defineFilters, useFilterState } from "@/components/filters";
import type { FilterValue } from "@/components/filters/filter-schema";
import { ResourceListPage } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { ErrorAlert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useCustomDomain } from "@/hooks/useToolsetUrl";
import { dateTimeFormatters, HumanizeDateTime } from "@/lib/dates";
import type { Skill } from "@gram/client/models/components/skill.js";
import type {
  Classifications,
  SourceKinds,
} from "@gram/client/models/operations/listskills.js";
import { useSkillEfficacyInsights } from "@gram/client/react-query/skillEfficacyInsights.js";
import { useSkillTags } from "@gram/client/react-query/skillTags.js";
import {
  invalidateAllRiskListPolicies,
  useRiskListPolicies,
} from "@gram/client/react-query/riskListPolicies.js";
import {
  useSkills,
  useSkillsInfinite,
} from "@gram/client/react-query/skills.js";
import { Badge } from "@/components/ui/Badge";
import { Icon } from "@/components/ui/Icon";
import { type Column, type SortDescriptor, Table } from "@/components/ui/Table";
import { sortTableData } from "@/components/ui/Table/sorting";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { useRoutes } from "@/routes";
import { useQueryState } from "nuqs";
import { useQueryClient } from "@tanstack/react-query";
import { useDeferredValue, useEffect, useMemo, useState } from "react";
import { Link, Navigate, useNavigate } from "react-router";
import { toast } from "sonner";
import { skillShareDomain, skillShareUrl } from "./share-link";
import { SkillManifestDialog } from "./SkillManifestDialog";
import {
  SKILL_CLASSIFICATION_OPTIONS,
  SKILL_SOURCE_OPTIONS,
} from "./skill-badge-options";
import { SkillSourceBadge } from "./skill-badges";
import { UnknownSkillActivationsSection } from "./UnknownSkillActivationsSection";
import { useDrainSkillPages } from "./use-drain-skill-pages";
import { useOpenSkillSuggestions } from "./use-open-skill-suggestions";

const SKILL_FILTERS = defineFilters([
  { id: "sourceKind", label: "Source", kind: "multiselect", pinned: true },
  {
    id: "classification",
    label: "Classification",
    kind: "multiselect",
    pinned: true,
  },
  {
    id: "tags",
    label: "Tags",
    kind: "multiselect",
    pinned: true,
    allLabel: "All tags",
  },
]);

const FILTER_OPTIONS = {
  sourceKind: [...SKILL_SOURCE_OPTIONS],
  classification: [...SKILL_CLASSIFICATION_OPTIONS],
};

function ColumnHeaderTooltip({
  label,
  tooltip,
}: {
  label: string;
  tooltip: string;
}): JSX.Element {
  return (
    <SimpleTooltip tooltip={tooltip}>
      <span
        tabIndex={0}
        className="cursor-help underline decoration-dotted underline-offset-2"
      >
        {label}
      </span>
    </SimpleTooltip>
  );
}

const RESULT_PAGE_SIZE = 50;
const METRIC_SORT_BATCH_SIZE = 200;
const EMPTY_SKILLS: Skill[] = [];
const METRIC_SORT_IDS = [
  "activations",
  "efficacy",
  "estimatedSavings",
] as const;

function formatEfficacy(value: number): string {
  return `${(value * 100).toFixed(1)}%`;
}

function formatSavings(minutes: number): string {
  if (minutes < 60) return `${minutes.toFixed(minutes < 10 ? 1 : 0)} min`;
  return `${(minutes / 60).toFixed(1)} hr`;
}

function noResultsMessage(active: boolean, incomplete: boolean): string {
  if (incomplete) return "Search incomplete. Retry to check remaining skills.";
  if (active) return "No matching skills.";
  return "No skills yet.";
}

export default function SkillsList(): JSX.Element {
  const routes = useRoutes();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const filters = useFilterState(SKILL_FILTERS);
  const [search, setSearch] = useState("");
  const [sort, setSort] = useState<SortDescriptor | null>(null);
  const deferredSearch = useDeferredValue(search);
  const [dialogOpen, setDialogOpen] = useState(false);
  // Legacy deep links opened the skill as a sheet via ?skill=<id>; redirect
  // them to the dedicated detail page.
  const [legacySkillId] = useQueryState("skill");
  const [page, setPage] = useState(0);
  const [pageCursors, setPageCursors] = useState<(string | undefined)[]>([
    undefined,
  ]);
  const metricSort =
    sort &&
    METRIC_SORT_IDS.includes(sort.id as (typeof METRIC_SORT_IDS)[number])
      ? sort
      : null;
  const searchQuery = deferredSearch.trim() || undefined;
  const sourceKinds = filters.values.sourceKind as SourceKinds[];
  const classifications = filters.values.classification as Classifications[];
  const tags = filters.values.tags as string[];
  const tagsQuery = useSkillTags(undefined, undefined, { throwOnError: false });
  const filterOptions = useMemo(
    () => ({
      ...FILTER_OPTIONS,
      tags: (tagsQuery.data?.tags ?? []).map((tag) => ({
        value: tag,
        label: tag,
      })),
    }),
    [tagsQuery.data?.tags],
  );
  const pageQuery = useSkills(
    {
      cursor: pageCursors[page],
      limit: RESULT_PAGE_SIZE,
      search: searchQuery,
      sourceKinds,
      classifications,
      tags,
      sort: "updated",
    },
    undefined,
    { throwOnError: false, enabled: !metricSort },
  );
  const metricQuery = useSkillsInfinite(
    {
      limit: METRIC_SORT_BATCH_SIZE,
      search: searchQuery,
      sourceKinds,
      classifications,
      tags,
    },
    undefined,
    {
      throwOnError: false,
      enabled: metricSort !== null,
    },
  );
  const metricSkills = useMemo(
    () =>
      metricQuery.data?.pages.flatMap((skillPage) => skillPage.result.skills) ??
      [],
    [metricQuery.data?.pages],
  );
  const insightSkills = metricSort
    ? metricSkills
    : (pageQuery.data?.result.skills ?? EMPTY_SKILLS);
  const insightsQuery = useSkillEfficacyInsights(
    // The list only shows activations, efficacy, and estimated savings; it never
    // displays attributed session cost. Skipping it avoids a full-project
    // telemetry scan that made this table take ~1 minute to load.
    metricSort
      ? { includeCosts: false }
      : {
          skillIds: insightSkills.map((skill) => skill.id),
          includeCosts: false,
        },
    undefined,
    {
      throwOnError: false,
      enabled: insightSkills.length > 0,
    },
  );
  const openSuggestions = useOpenSkillSuggestions();
  const metricsBySkill = useMemo(
    () =>
      new Map(
        insightsQuery.data?.result.insights.map((insight) => [
          insight.skillId,
          insight.metrics,
        ]) ?? [],
      ),
    [insightsQuery.data?.result.insights],
  );
  const active =
    deferredSearch.trim().length > 0 ||
    filters.values.sourceKind.length > 0 ||
    filters.values.classification.length > 0 ||
    filters.values.tags.length > 0;
  const insightsUnavailable = !!insightsQuery.error && !insightsQuery.data;
  const effectiveMetricSort = metricSort !== null && !insightsUnavailable;
  const effectiveSort = insightsUnavailable && metricSort ? null : sort;
  const skills = effectiveMetricSort
    ? metricSkills
    : (pageQuery.data?.result.skills ?? EMPTY_SKILLS);

  // Copied share links point at the org's custom domain when one is live.
  // Only pay for the domains request when a listed skill is actually shared.
  const { domain: customDomain } = useCustomDomain(
    skills.some((skill) => !!skill.shareToken),
  );
  const shareDomain = skillShareDomain(customDomain);

  const columns: Column<Skill>[] = [
    {
      key: "name",
      header: "Skill",
      width: "2fr",
      sortable: true,
      sortValue: (skill) => skill.displayName.toLocaleLowerCase(),
      render: (skill) => (
        <div className="min-w-0">
          <Link
            to={routes.skills.detail.href(skill.id)}
            className="font-medium hover:underline"
            onClick={(e) => e.stopPropagation()}
          >
            {skill.displayName}
          </Link>
          {openSuggestions.skillIds.has(skill.id) && (
            <Badge variant="information" className="ml-2">
              Suggested edit
            </Badge>
          )}
          <Text small muted className="truncate font-mono">
            {skill.name}
          </Text>
          {skill.tags.length > 0 && (
            <div className="mt-1 flex flex-wrap gap-1">
              {skill.tags.map((tag) => (
                <Badge key={tag} variant="neutral" className="text-xs">
                  {tag}
                </Badge>
              ))}
            </div>
          )}
        </div>
      ),
    },
    {
      key: "source",
      header: (
        <ColumnHeaderTooltip
          label="Source"
          tooltip="How the skill entered the registry: added manually, or captured from agent use."
        />
      ),
      width: "0.8fr",
      sortable: true,
      sortLabel: "Source",
      sortValue: (skill) =>
        SKILL_SOURCE_OPTIONS.find((option) => option.value === skill.sourceKind)
          ?.label ?? skill.sourceKind,
      render: (skill) => <SkillSourceBadge value={skill.sourceKind} />,
    },
    {
      key: "activations",
      header: "Activations (30d)",
      width: "0.8fr",
      sortable: true,
      sortValue: (skill) => metricsBySkill.get(skill.id)?.activations ?? 0,
      render: (skill) => (
        <Text small className="tabular-nums">
          {metricsBySkill.get(skill.id)?.activations.toLocaleString() ??
            (insightsQuery.data ? "0" : "-")}
        </Text>
      ),
    },
    {
      key: "efficacy",
      header: (
        <ColumnHeaderTooltip
          label="Efficacy"
          tooltip="Average usefulness score from sampled sessions that used this skill."
        />
      ),
      width: "0.8fr",
      sortable: true,
      sortLabel: "Efficacy",
      sortValue: (skill) =>
        metricsBySkill.get(skill.id)?.efficacy?.averageScore,
      render: (skill) => {
        const efficacy = metricsBySkill.get(skill.id)?.efficacy;
        return (
          <Text
            small
            className="tabular-nums"
            title={
              efficacy
                ? `${efficacy.scoredSessions.toLocaleString()} sampled sessions`
                : "No sampled scores"
            }
          >
            {efficacy ? formatEfficacy(efficacy.averageScore) : "-"}
          </Text>
        );
      },
    },
    {
      key: "estimatedSavings",
      header: (
        <ColumnHeaderTooltip
          label="Estimated savings"
          tooltip="Estimated time saved across scored sessions that used this skill."
        />
      ),
      width: "0.8fr",
      sortable: true,
      sortLabel: "Estimated savings",
      sortValue: (skill) =>
        metricsBySkill.get(skill.id)?.efficacy?.estimatedMinutesSavedTotal,
      render: (skill) => {
        const efficacy = metricsBySkill.get(skill.id)?.efficacy;
        return (
          <Text small className="tabular-nums">
            {efficacy
              ? formatSavings(efficacy.estimatedMinutesSavedTotal)
              : "-"}
          </Text>
        );
      },
    },
    {
      key: "updated",
      header: "Updated",
      width: "0.8fr",
      sortable: true,
      sortValue: (skill) => skill.updatedAt,
      render: (skill) => (
        <Text
          small
          muted
          title={dateTimeFormatters.full.format(skill.updatedAt)}
        >
          <HumanizeDateTime date={skill.updatedAt} />
        </Text>
      ),
    },
    {
      key: "share",
      header: "",
      width: "48px",
      render: (skill) =>
        skill.shareToken ? (
          <CopyButton
            size="sm"
            text={skillShareUrl(skill.shareToken, shareDomain)}
            tooltip="Copy public link"
            onCopy={() => {
              toast.success("Public link copied");
            }}
          />
        ) : null,
    },
    {
      key: "actions",
      header: "",
      width: "48px",
      render: () => (
        <Icon
          name="arrow-right"
          className="text-muted-foreground h-4 w-4"
          aria-hidden
        />
      ),
    },
  ];

  const visibleSkills = sortTableData(
    skills,
    columns,
    effectiveSort,
  ) as Skill[];

  useEffect(() => {
    if (!insightsUnavailable || !metricSort) return;
    setSort(null);
    setPage(0);
    setPageCursors([undefined]);
  }, [insightsUnavailable, metricSort]);

  useDrainSkillPages({
    active: effectiveMetricSort,
    hasNextPage: metricQuery.hasNextPage,
    isFetchingNextPage: metricQuery.isFetchingNextPage,
    isFetchNextPageError: metricQuery.isFetchNextPageError,
    fetchNextPage: metricQuery.fetchNextPage,
  });

  const displayedSkills = effectiveMetricSort
    ? visibleSkills.slice(
        page * RESULT_PAGE_SIZE,
        (page + 1) * RESULT_PAGE_SIZE,
      )
    : visibleSkills;
  const totalCount = effectiveMetricSort
    ? (metricQuery.data?.pages[0]?.result.totalCount ?? 0)
    : (pageQuery.data?.result.totalCount ?? 0);
  const paginationCount =
    effectiveMetricSort && metricQuery.isFetchNextPageError
      ? visibleSkills.length
      : totalCount;
  const totalPages = Math.ceil(paginationCount / RESULT_PAGE_SIZE);
  const query = effectiveMetricSort ? metricQuery : pageQuery;
  const isEmptyProject =
    !!query.data && totalCount === 0 && !active && !query.isFetching;
  const draining =
    effectiveMetricSort &&
    metricQuery.hasNextPage &&
    !metricQuery.isFetchNextPageError;
  const resetPage = () => {
    setPage(0);
    setPageCursors([undefined]);
  };
  const nextPage = () => {
    if (effectiveMetricSort) {
      setPage((current) => current + 1);
      return;
    }
    const nextCursor = pageQuery.data?.result.nextCursor;
    if (!nextCursor) return;
    setPageCursors((current) => [...current.slice(0, page + 1), nextCursor]);
    setPage((current) => current + 1);
  };

  if (legacySkillId) {
    return <Navigate to={routes.skills.detail.href(legacySkillId)} replace />;
  }

  return (
    <ResourceListPage
      title="Skills"
      description="Record, inspect, and version the skills available to this project."
      primaryAction={<AddSkillButton onClick={() => setDialogOpen(true)} />}
      hideToolbar={isEmptyProject}
      search={{
        value: search,
        onChange: (value) => {
          setSearch(value);
          resetPage();
        },
        debounceMs: 150,
        placeholder: "Search skills",
      }}
      filters={{
        schema: SKILL_FILTERS,
        values: filters.values,
        optionsById: filterOptions,
        onChange: (id, value) => {
          (filters.setValue as (id: string, value: FilterValue) => void)(
            id,
            value,
          );
          resetPage();
        },
        onClear: (id) => {
          (filters.clearValue as (id: string) => void)(id);
          resetPage();
        },
        onClearAll: () => {
          filters.clearAll();
          resetPage();
        },
      }}
      onRefresh={() => {
        void Promise.all([
          effectiveMetricSort ? metricQuery.refetch() : pageQuery.refetch(),
          insightsQuery.refetch(),
          openSuggestions.query.refetch(),
          tagsQuery.refetch(),
          invalidateAllRiskListPolicies(queryClient),
        ]);
      }}
      isRefreshing={
        pageQuery.isFetching ||
        metricQuery.isFetching ||
        insightsQuery.isFetching ||
        openSuggestions.query.isFetching ||
        tagsQuery.isFetching
      }
    >
      <div className="space-y-4">
        <RequireScope scope="org:admin" level="section">
          <SkillPromptInjectionPolicyCard />
        </RequireScope>

        {draining && (
          <Text small muted role="status" aria-live="polite">
            Loading all skills to finish this view...
          </Text>
        )}

        {openSuggestions.query.error && (
          <div className="space-y-2">
            <ErrorAlert
              title="Unable to load suggested edits"
              error={openSuggestions.query.error}
            />
            <Button
              size="sm"
              variant="secondary"
              onClick={() => void openSuggestions.query.refetch()}
            >
              Retry suggested edits
            </Button>
          </div>
        )}

        {openSuggestions.total > 0 &&
          !openSuggestions.fullyLoaded &&
          !openSuggestions.query.error && (
            <Text small muted role="status" aria-live="polite">
              Loading all suggested edits...
            </Text>
          )}

        {insightsUnavailable && (
          <div className="space-y-2">
            <ErrorAlert
              title="Unable to load skill insights"
              error={insightsQuery.error}
            />
            <Button
              size="sm"
              variant="secondary"
              onClick={() => void insightsQuery.refetch()}
            >
              Retry insights
            </Button>
          </div>
        )}

        {query.isPending && !query.data && <SkeletonTable />}
        {query.error && !query.data && (
          <ErrorAlert
            title="Unable to load skills"
            error={query.error instanceof Error ? query.error : "Try again."}
          />
        )}
        {isEmptyProject && (
          <SkillsEmptyState onAdd={() => setDialogOpen(true)} />
        )}
        {query.data && !isEmptyProject && !draining && (
          <div className="overflow-x-auto">
            <Table
              columns={columns}
              data={displayedSkills}
              rowKey={(skill) => skill.id}
              sort={effectiveSort}
              onSortChange={(nextSort) => {
                setSort(nextSort);
                resetPage();
              }}
              onRowClick={(skill) =>
                void navigate(routes.skills.detail.href(skill.id))
              }
              className="min-w-[1100px]"
              noResultsMessage={noResultsMessage(
                active,
                effectiveMetricSort && metricQuery.isFetchNextPageError,
              )}
            />
          </div>
        )}

        {effectiveMetricSort && metricQuery.isFetchNextPageError && (
          <LoadMoreError onRetry={() => void metricQuery.fetchNextPage()} />
        )}

        {!draining && totalPages > 1 && (
          <div className="flex items-center justify-between border-t px-4 py-3">
            <Text small muted>
              {page * RESULT_PAGE_SIZE + 1}-
              {Math.min((page + 1) * RESULT_PAGE_SIZE, paginationCount)} of{" "}
              {paginationCount}
            </Text>
            <div className="flex items-center gap-2">
              <Button
                size="sm"
                variant="secondary"
                onClick={() => setPage((current) => current - 1)}
                disabled={page === 0}
              >
                Previous
              </Button>
              <Button
                size="sm"
                variant="secondary"
                onClick={nextPage}
                disabled={page >= totalPages - 1}
              >
                Next
              </Button>
            </div>
          </div>
        )}

        <UnknownSkillActivationsSection />
      </div>

      <SkillManifestDialog
        mode="create"
        open={dialogOpen}
        onOpenChange={setDialogOpen}
      />
    </ResourceListPage>
  );
}

function SkillPromptInjectionPolicyCard(): JSX.Element | null {
  const routes = useRoutes();
  const policiesQuery = useRiskListPolicies(undefined, undefined, {
    throwOnError: false,
  });
  if (policiesQuery.error && !policiesQuery.data) {
    return (
      <div className="space-y-2">
        <ErrorAlert
          title="Unable to load prompt injection policy"
          error={policiesQuery.error}
        />
        <Button
          size="sm"
          variant="secondary"
          onClick={() => void policiesQuery.refetch()}
        >
          Retry policy status
        </Button>
      </div>
    );
  }
  if (!policiesQuery.data) return null;

  const policy = policiesQuery.data.policies.find(
    (candidate) =>
      candidate.enabled && candidate.sources.includes("prompt_injection"),
  );
  const action = policy ? (
    <routes.policyCenter.detail.Link params={[policy.id]}>
      <Button size="sm" variant="secondary">
        View policy
      </Button>
    </routes.policyCenter.detail.Link>
  ) : (
    <routes.policyCenter.new.Link
      queryParams={{ kind: "standard", category: "prompt_injection" }}
    >
      <Button size="sm" variant="secondary">
        Set up scanning
      </Button>
    </routes.policyCenter.new.Link>
  );

  return (
    <div className="border-border bg-muted/20 flex flex-wrap items-center justify-between gap-4 border p-4">
      <div className="space-y-1">
        <Text variant="subheading">
          {policy
            ? "Prompt injection scanning configured"
            : "Set up prompt injection scanning"}
        </Text>
        <Text small muted>
          {policy
            ? "Captured skill manifests are checked by an enabled policy."
            : "Add a policy to check future captured skill manifests for hidden instructions."}
        </Text>
      </div>
      {action}
    </div>
  );
}

function AddSkillButton({ onClick }: { onClick: () => void }): JSX.Element {
  const project = useProject();
  return (
    <RequireScope
      scope="skill:write"
      resourceId={project.id}
      level="component"
      reason="You need write access to add skills."
    >
      <Button icon="plus" onClick={onClick}>
        Add Skill
      </Button>
    </RequireScope>
  );
}

function SkillsEmptyState({ onAdd }: { onAdd: () => void }): JSX.Element {
  return (
    <div className="bg-muted/20 flex flex-col items-center justify-center border border-dashed px-8 py-16">
      <div className="border-border mb-4 flex h-12 w-12 items-center justify-center border">
        <Icon name="terminal" className="text-muted-foreground h-6 w-6" />
      </div>
      <Text variant="subheading" className="mb-1">
        No skills yet
      </Text>
      <Text small muted className="mb-4 max-w-md text-center">
        Skills are reusable instructions your agents can load on demand. Add
        your first skill to start versioning it here.
      </Text>
      <AddSkillButton onClick={onAdd} />
    </div>
  );
}

function LoadMoreError({ onRetry }: { onRetry: () => void }): JSX.Element {
  return (
    <div className="border-destructive/40 bg-destructive/5 flex flex-wrap items-center justify-between gap-3 border p-3">
      <Text small className="text-destructive">
        Unable to load more skills.
      </Text>
      <Button size="sm" variant="secondary" onClick={onRetry}>
        Retry
      </Button>
    </div>
  );
}
