import { defineFilters, useFilterState } from "@/components/filters";
import type { FilterValue } from "@/components/filters/filter-schema";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { ErrorAlert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { SkeletonTable } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import { dateTimeFormatters, HumanizeDateTime } from "@/lib/dates";
import type { Skill } from "@gram/client/models/components/skill.js";
import { useSkillsInfinite } from "@gram/client/react-query/skills.js";
import { type Column, Icon, Table } from "@speakeasy-api/moonshine";
import { useRoutes } from "@/routes";
import { useQueryState } from "nuqs";
import { useDeferredValue, useMemo, useState } from "react";
import { Link, Navigate, useNavigate } from "react-router";
import { SkillManifestDialog } from "./SkillManifestDialog";
import {
  SKILL_CLASSIFICATION_OPTIONS,
  SKILL_SOURCE_OPTIONS,
} from "./skill-badge-options";
import { SkillClassificationBadge, SkillSourceBadge } from "./skill-badges";
import { filterSkills, skillCountLabel } from "./skills-list-helpers";
import { UnknownSkillActivationsSection } from "./UnknownSkillActivationsSection";
import { useDrainSkillPages } from "./use-drain-skill-pages";

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

const RESULT_PAGE_SIZE = 200;

function noResultsMessage(
  draining: boolean,
  active: boolean,
  incomplete: boolean,
): string {
  if (draining) return "Loading remaining skills...";
  if (incomplete) return "Search incomplete. Retry to check remaining skills.";
  if (active) return "No matching skills.";
  return "No skills yet.";
}

export default function SkillsList(): JSX.Element {
  const routes = useRoutes();
  const navigate = useNavigate();
  const filters = useFilterState(SKILL_FILTERS);
  const [search, setSearch] = useState("");
  const deferredSearch = useDeferredValue(search);
  const [dialogOpen, setDialogOpen] = useState(false);
  // Legacy deep links opened the skill as a sheet via ?skill=<id>; redirect
  // them to the dedicated detail page.
  const [legacySkillId] = useQueryState("skill");
  const [displayCount, setDisplayCount] = useState(RESULT_PAGE_SIZE);
  const query = useSkillsInfinite({ limit: 200 }, undefined, {
    throwOnError: false,
  });
  const skills = useMemo(
    () => query.data?.pages.flatMap((page) => page.result.skills) ?? [],
    [query.data?.pages],
  );
  const active =
    deferredSearch.trim().length > 0 ||
    filters.values.sourceKind.length > 0 ||
    filters.values.classification.length > 0;
  const visibleSkills = useMemo(
    () =>
      filterSkills(
        skills,
        deferredSearch,
        filters.values.sourceKind,
        filters.values.classification,
      ),
    [
      deferredSearch,
      filters.values.classification,
      filters.values.sourceKind,
      skills,
    ],
  );

  useDrainSkillPages({
    active,
    hasNextPage: query.hasNextPage,
    isFetchingNextPage: query.isFetchingNextPage,
    isFetchNextPageError: query.isFetchNextPageError,
    fetchNextPage: query.fetchNextPage,
  });

  const displayedSkills = visibleSkills.slice(0, displayCount);
  const isEmptyProject =
    !!query.data && skills.length === 0 && !active && !query.hasNextPage;

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
      width: "100px",
      render: (skill) => <Type small>{skill.versionCount}</Type>,
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

  const countLabel = skillCountLabel({
    active,
    hasNextPage: query.hasNextPage,
    incomplete: query.isFetchNextPageError,
    loadedCount: skills.length,
    resultCount: visibleSkills.length,
  });
  const draining = active && query.hasNextPage && !query.isFetchNextPageError;

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
                      setDisplayCount(RESULT_PAGE_SIZE);
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
                      setDisplayCount(RESULT_PAGE_SIZE);
                    }}
                    onClear={(id) => {
                      (filters.clearValue as (id: string) => void)(id);
                      setDisplayCount(RESULT_PAGE_SIZE);
                    }}
                    onClearAll={() => {
                      filters.clearAll();
                      setDisplayCount(RESULT_PAGE_SIZE);
                    }}
                  />
                  <Page.Toolbar.Count>{countLabel}</Page.Toolbar.Count>
                  <Page.Toolbar.Refresh
                    onRefresh={() => void query.refetch()}
                    isRefreshing={query.isFetching && !query.isFetchingNextPage}
                  />
                </Page.Toolbar>
              )}

              {draining && (
                <Type small muted role="status" aria-live="polite">
                  Loading all skills to finish this search...
                </Type>
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
              {query.data && !isEmptyProject && (
                <div className="overflow-x-auto">
                  <Table
                    columns={columns}
                    data={displayedSkills}
                    rowKey={(skill) => skill.id}
                    onRowClick={(skill) =>
                      void navigate(routes.skills.detail.href(skill.id))
                    }
                    className="min-w-[900px]"
                    noResultsMessage={noResultsMessage(
                      draining,
                      active,
                      query.isFetchNextPageError,
                    )}
                  />
                </div>
              )}

              {query.isFetchNextPageError && (
                <LoadMoreError onRetry={() => void query.fetchNextPage()} />
              )}

              {displayedSkills.length < visibleSkills.length && (
                <div className="flex justify-center">
                  <Button
                    variant="outline"
                    onClick={() =>
                      setDisplayCount((count) => count + RESULT_PAGE_SIZE)
                    }
                  >
                    Show more results
                  </Button>
                </div>
              )}

              {query.hasNextPage && !query.isFetchNextPageError && (
                <div className="flex justify-center">
                  <Button
                    variant="outline"
                    disabled={query.isFetchingNextPage}
                    onClick={() => void query.fetchNextPage()}
                  >
                    {query.isFetchingNextPage ? "Loading..." : "Load more"}
                  </Button>
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
      <Button size="sm" variant="outline" onClick={onRetry}>
        Retry
      </Button>
    </div>
  );
}
