import { defineFilters, useFilterState } from "@/components/filters";
import type { FilterValue } from "@/components/filters/filter-schema";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { ErrorAlert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Type } from "@/components/ui/Type";
import { useProject } from "@/contexts/Auth";
import { dateTimeFormatters, HumanizeDateTime } from "@/lib/dates";
import type { Skill } from "@gram/client/models/components/skill.js";
import type {
  Classifications,
  SourceKinds,
} from "@gram/client/models/operations/listskills.js";
import { useSkillEfficacyInsights } from "@gram/client/react-query/skillEfficacyInsights.js";
import {
  useSkills,
  useSkillsInfinite,
} from "@gram/client/react-query/skills.js";
import { Badge } from "@/components/ui/Badge";
import { Icon } from "@/components/ui/Icon";
import { type Column, Table } from "@/components/ui/Table";
import { useRoutes } from "@/routes";
import { useQueryState } from "nuqs";
import { useDeferredValue, useEffect, useMemo, useState } from "react";
import { Link, Navigate, useNavigate } from "react-router";
import { toast } from "sonner";
import { ApproveAllSkillSuggestions } from "./ApproveAllSkillSuggestions";
import { skillShareUrl } from "./share-link";
import { SkillManifestDialog } from "./SkillManifestDialog";
import {
  SKILL_CLASSIFICATION_OPTIONS,
  SKILL_SOURCE_OPTIONS,
} from "./skill-badge-options";
import { SkillClassificationBadge, SkillSourceBadge } from "./skill-badges";
import { type SkillSort, sortSkills } from "./skills-list-helpers";
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
]);

const FILTER_OPTIONS = {
  sourceKind: SKILL_SOURCE_OPTIONS,
  classification: SKILL_CLASSIFICATION_OPTIONS,
};

const RESULT_PAGE_SIZE = 50;
const METRIC_SORT_BATCH_SIZE = 200;
const EMPTY_SKILLS: Skill[] = [];
const INSIGHT_SORT_OPTIONS = [
  { value: "updated", label: "Recently updated" },
  { value: "activations", label: "Most activated" },
  { value: "efficacy", label: "Highest efficacy" },
  { value: "estimated_savings", label: "Most estimated savings" },
];

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
  const filters = useFilterState(SKILL_FILTERS);
  const [search, setSearch] = useState("");
  const [sort, setSort] = useState<SkillSort>("updated");
  const deferredSearch = useDeferredValue(search);
  const [dialogOpen, setDialogOpen] = useState(false);
  // Legacy deep links opened the skill as a sheet via ?skill=<id>; redirect
  // them to the dedicated detail page.
  const [legacySkillId] = useQueryState("skill");
  const [page, setPage] = useState(0);
  const [pageCursors, setPageCursors] = useState<(string | undefined)[]>([
    undefined,
  ]);
  const metricSort = sort !== "updated";
  const searchQuery = deferredSearch.trim() || undefined;
  const sourceKinds = filters.values.sourceKind as SourceKinds[];
  const classifications = filters.values.classification as Classifications[];
  const pageQuery = useSkills(
    {
      cursor: pageCursors[page],
      limit: RESULT_PAGE_SIZE,
      search: searchQuery,
      sourceKinds,
      classifications,
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
    },
    undefined,
    {
      throwOnError: false,
      enabled: metricSort,
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
    metricSort ? {} : { skillIds: insightSkills.map((skill) => skill.id) },
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
    filters.values.classification.length > 0;
  const insightsUnavailable = !!insightsQuery.error && !insightsQuery.data;
  const effectiveMetricSort = metricSort && !insightsUnavailable;
  const effectiveSort = insightsUnavailable ? "updated" : sort;
  const skills = effectiveMetricSort
    ? metricSkills
    : (pageQuery.data?.result.skills ?? EMPTY_SKILLS);
  const visibleSkills = useMemo(
    () =>
      effectiveMetricSort
        ? sortSkills(skills, metricsBySkill, effectiveSort)
        : skills,
    [effectiveMetricSort, effectiveSort, metricsBySkill, skills],
  );

  useEffect(() => {
    if (!insightsUnavailable || sort === "updated") return;
    setSort("updated");
    setPage(0);
    setPageCursors([undefined]);
  }, [insightsUnavailable, sort]);

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

  const columns: Column<Skill>[] = [
    {
      key: "name",
      header: "Skill",
      width: "1.5fr",
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
          <Type small muted className="truncate font-mono">
            {skill.name}
          </Type>
        </div>
      ),
    },
    {
      key: "summary",
      header: "Summary",
      width: "2fr",
      render: (skill) => (
        <Type small muted className="line-clamp-2">
          {skill.summary || "No summary"}
        </Type>
      ),
    },
    {
      key: "source",
      header: "Source",
      width: "120px",
      render: (skill) => <SkillSourceBadge value={skill.sourceKind} />,
    },
    {
      key: "classification",
      header: "Classification",
      width: "130px",
      render: (skill) => (
        <SkillClassificationBadge value={skill.classification} />
      ),
    },
    {
      key: "versions",
      header: "Versions",
      width: "90px",
      render: (skill) => <Type small>{skill.versionCount}</Type>,
    },
    {
      key: "activations",
      header: "Activations (30d)",
      width: "130px",
      render: (skill) => (
        <Type small className="tabular-nums">
          {metricsBySkill.get(skill.id)?.activations.toLocaleString() ??
            (insightsQuery.data ? "0" : "-")}
        </Type>
      ),
    },
    {
      key: "efficacy",
      header: "Efficacy",
      width: "110px",
      render: (skill) => {
        const efficacy = metricsBySkill.get(skill.id)?.efficacy;
        return (
          <Type
            small
            className="tabular-nums"
            title={
              efficacy
                ? `${efficacy.scoredSessions.toLocaleString()} sampled sessions`
                : "No sampled scores"
            }
          >
            {efficacy ? formatEfficacy(efficacy.averageScore) : "-"}
          </Type>
        );
      },
    },
    {
      key: "estimatedSavings",
      header: "Estimated savings",
      width: "150px",
      render: (skill) => {
        const efficacy = metricsBySkill.get(skill.id)?.efficacy;
        return (
          <Type small className="tabular-nums">
            {efficacy
              ? formatSavings(efficacy.estimatedMinutesSavedTotal)
              : "-"}
          </Type>
        );
      },
    },
    {
      key: "updated",
      header: "Updated",
      width: "150px",
      render: (skill) => (
        <Type
          small
          muted
          title={dateTimeFormatters.full.format(skill.updatedAt)}
        >
          <HumanizeDateTime date={skill.updatedAt} />
        </Type>
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
            text={skillShareUrl(skill.shareToken)}
            tooltip="Copy public link"
            onCopy={() => {
              toast.success("Public link copied");
            }}
          />
        ) : null,
    },
    {
      key: "open",
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

  const countLabel = `${totalCount} skill${totalCount === 1 ? "" : "s"}`;

  if (legacySkillId) {
    return <Navigate to={routes.skills.detail.href(legacySkillId)} replace />;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title>Skills</Page.Section.Title>
          <Page.Section.Description>
            Record, inspect, and version the skills available to this project.
          </Page.Section.Description>
          <Page.Section.CTA>
            <AddSkillButton onClick={() => setDialogOpen(true)} />
          </Page.Section.CTA>
          <Page.Section.Body>
            <div className="space-y-4">
              {!isEmptyProject && (
                <Page.Toolbar>
                  <Page.Toolbar.Search
                    value={search}
                    onChange={(value) => {
                      setSearch(value);
                      resetPage();
                    }}
                    debounceMs={150}
                    placeholder="Search skills"
                  />
                  <Page.Toolbar.Filters
                    schema={SKILL_FILTERS}
                    values={filters.values}
                    optionsById={FILTER_OPTIONS}
                    onChange={(id, value) => {
                      (
                        filters.setValue as (
                          id: string,
                          value: FilterValue,
                        ) => void
                      )(id, value);
                      resetPage();
                    }}
                    onClear={(id) => {
                      (filters.clearValue as (id: string) => void)(id);
                      resetPage();
                    }}
                    onClearAll={() => {
                      filters.clearAll();
                      resetPage();
                    }}
                  />
                  <Page.Toolbar.Count>{countLabel}</Page.Toolbar.Count>
                  <Page.Toolbar.SortBy
                    value={effectiveSort}
                    onChange={(value) => {
                      setSort(value as SkillSort);
                      resetPage();
                    }}
                    options={INSIGHT_SORT_OPTIONS}
                  />
                  <Page.Toolbar.Actions>
                    <ApproveAllSkillSuggestions
                      suggestions={openSuggestions.suggestions}
                      total={openSuggestions.total}
                      fullyLoaded={openSuggestions.fullyLoaded}
                    />
                  </Page.Toolbar.Actions>
                  <Page.Toolbar.Refresh
                    onRefresh={() => {
                      void Promise.all([
                        effectiveMetricSort
                          ? metricQuery.refetch()
                          : pageQuery.refetch(),
                        insightsQuery.refetch(),
                        openSuggestions.query.refetch(),
                      ]);
                    }}
                    isRefreshing={
                      pageQuery.isFetching ||
                      metricQuery.isFetching ||
                      insightsQuery.isFetching ||
                      openSuggestions.query.isFetching
                    }
                  />
                </Page.Toolbar>
              )}

              {draining && (
                <Type small muted role="status" aria-live="polite">
                  Loading all skills to finish this view...
                </Type>
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
                  <Type small muted role="status" aria-live="polite">
                    Loading all suggested edits...
                  </Type>
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
                  error={
                    query.error instanceof Error ? query.error : "Try again."
                  }
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
                <LoadMoreError
                  onRetry={() => void metricQuery.fetchNextPage()}
                />
              )}

              {!draining && totalPages > 1 && (
                <div className="flex items-center justify-between border-t px-4 py-3">
                  <Type small muted>
                    {page * RESULT_PAGE_SIZE + 1}-
                    {Math.min((page + 1) * RESULT_PAGE_SIZE, totalCount)} of{" "}
                    {totalCount}
                  </Type>
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
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
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
        Add skill
      </Button>
    </RequireScope>
  );
}

function SkillsEmptyState({ onAdd }: { onAdd: () => void }): JSX.Element {
  return (
    <div className="bg-muted/20 flex flex-col items-center justify-center rounded-xl border border-dashed px-8 py-16">
      <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
        <Icon name="terminal" className="text-muted-foreground h-6 w-6" />
      </div>
      <Type variant="subheading" className="mb-1">
        No skills yet
      </Type>
      <Type small muted className="mb-4 max-w-md text-center">
        Skills are reusable instructions your agents can load on demand. Add
        your first skill to start versioning it here.
      </Type>
      <AddSkillButton onClick={onAdd} />
    </div>
  );
}

function LoadMoreError({ onRetry }: { onRetry: () => void }): JSX.Element {
  return (
    <div className="border-destructive/40 bg-destructive/5 flex flex-wrap items-center justify-between gap-3 rounded-lg border p-3">
      <Type small className="text-destructive">
        Unable to load more skills.
      </Type>
      <Button size="sm" variant="secondary" onClick={onRetry}>
        Retry
      </Button>
    </div>
  );
}
