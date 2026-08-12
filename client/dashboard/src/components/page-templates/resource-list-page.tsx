import { Page } from "@/components/page-layout";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { StatRow, type StatRowMetric } from "@/components/stat-row";
import { SkeletonTable } from "@/components/ui/Skeleton";
import type { IconName } from "@/components/ui/Icon/names";
import type { ViewMode } from "@/components/ui/ViewToggle/use-view-mode";
import { cn } from "@/lib/utils";
import type { ComponentProps, ReactNode } from "react";
import {
  TemplateFrame,
  TemplateHeader,
  type TemplateFrameProps,
  type TemplateHeaderProps,
} from "./scaffold";

type FiltersProps = ComponentProps<typeof Page.Toolbar.Filters>;

/** Inline empty-state config, or a fully custom node. */
type EmptyConfig =
  | {
      icon?: IconName;
      graphic?: ReactNode;
      heading: string;
      description?: string;
      action?: ReactNode;
    }
  | ReactNode;

/**
 * ResourceListPage — the paint-by-numbers template for a collection page:
 * header + toolbar (search / filters / sort / view / refresh) + a table or
 * card grid + empty state. It owns the `Page` frame, the scope gate, the
 * single header, the toolbar wiring, and the loading / empty branches, so a
 * page supplies only its data and its table columns or card renderer.
 *
 *   <ResourceListPage
 *     scope={["mcp:read", "mcp:write"]}
 *     title="MCP Servers"
 *     description="Servers exposed to your agents."
 *     primaryAction={<NewServerButton />}
 *     search={{ value: q, onChange: setQ, placeholder: "Search servers" }}
 *     viewToggle={{ value: view, onChange: setView }}
 *     isLoading={q.isPending}
 *     isEmpty={rows.length === 0}
 *     empty={{ icon: "server", heading: "No servers yet", action: <NewServerButton /> }}
 *   >
 *     <Table columns={columns} data={rows} rowKey={(r) => r.id} />
 *   </ResourceListPage>
 */
export function ResourceListPage({
  // frame
  scope,
  scopeAll,
  resourceId,
  breadcrumbSubstitutions,
  fullHeight,
  // header
  title,
  description,
  stage,
  area,
  primaryAction,
  // optional metric-header row (absorbs the Risk drill-down pattern)
  metrics,
  metricsLoading,
  // toolbar
  search,
  filters,
  sort,
  viewToggle,
  count,
  toolbarActions,
  onRefresh,
  isRefreshing,
  hideToolbar,
  // body
  isLoading = false,
  isEmpty = false,
  empty,
  loadingFallback,
  children,
  bodyClassName,
}: TemplateFrameProps &
  TemplateHeaderProps & {
    /**
     * Constrain the body to the viewport so a long table/grid scrolls inside
     * its own pane (mirrors the old `Page.Body fullHeight` contract) instead of
     * growing the whole page. Use for inventory tables with internal paging.
     */
    fullHeight?: boolean;
    metrics?: StatRowMetric[];
    metricsLoading?: boolean;
    search?: {
      value: string;
      onChange: (value: string) => void;
      placeholder?: string;
      debounceMs?: number;
    };
    filters?: FiltersProps;
    sort?: ComponentProps<typeof Page.Toolbar.SortBy>;
    viewToggle?: { value: ViewMode; onChange: (value: ViewMode) => void };
    count?: ReactNode;
    toolbarActions?: ReactNode;
    onRefresh?: () => void;
    isRefreshing?: boolean;
    /** Force-hide the toolbar even if controls are provided (e.g. while empty). */
    hideToolbar?: boolean;
    isLoading?: boolean;
    isEmpty?: boolean;
    empty?: EmptyConfig;
    /** Loading placeholder. Defaults to a shaped SkeletonTable. */
    loadingFallback?: ReactNode;
    children: ReactNode;
    bodyClassName?: string;
  }): JSX.Element {
  const hasToolbar =
    !hideToolbar &&
    (search != null ||
      filters != null ||
      sort != null ||
      viewToggle != null ||
      count != null ||
      toolbarActions != null ||
      onRefresh != null);

  return (
    <TemplateFrame
      scope={scope}
      scopeAll={scopeAll}
      resourceId={resourceId}
      breadcrumbSubstitutions={breadcrumbSubstitutions}
      fullHeight={fullHeight}
    >
      <TemplateHeader
        title={title}
        description={description}
        stage={stage}
        area={area}
        primaryAction={primaryAction}
      />

      {metrics != null && metrics.length > 0 && (
        <StatRow
          metrics={metrics}
          isLoading={metricsLoading}
          className="mb-6"
        />
      )}

      {hasToolbar && !isLoading && !isEmpty && (
        <Page.Toolbar className="mb-4">
          {search != null && <Page.Toolbar.Search {...search} />}
          {filters != null && <Page.Toolbar.Filters {...filters} />}
          {sort != null && <Page.Toolbar.SortBy {...sort} />}
          {count != null && <Page.Toolbar.Count>{count}</Page.Toolbar.Count>}
          {viewToggle != null && <Page.Toolbar.ViewAs {...viewToggle} />}
          {toolbarActions != null && (
            <Page.Toolbar.Actions>{toolbarActions}</Page.Toolbar.Actions>
          )}
          {onRefresh != null && (
            <Page.Toolbar.Refresh
              onRefresh={onRefresh}
              isRefreshing={isRefreshing}
            />
          )}
        </Page.Toolbar>
      )}

      <div
        className={cn(
          fullHeight && "flex min-h-0 flex-1 flex-col",
          bodyClassName,
        )}
      >
        <ResourceListBody
          isLoading={isLoading}
          isEmpty={isEmpty}
          empty={empty}
          loadingFallback={loadingFallback}
        >
          {children}
        </ResourceListBody>
      </div>
    </TemplateFrame>
  );
}

function ResourceListBody({
  isLoading,
  isEmpty,
  empty,
  loadingFallback,
  children,
}: {
  isLoading: boolean;
  isEmpty: boolean;
  empty?: EmptyConfig;
  loadingFallback?: ReactNode;
  children: ReactNode;
}): JSX.Element {
  if (isLoading) {
    return <>{loadingFallback ?? <SkeletonTable />}</>;
  }
  if (isEmpty && empty != null) {
    if (isEmptyConfig(empty)) {
      return (
        <InlineEmptyState
          icon={empty.icon}
          graphic={empty.graphic}
          heading={empty.heading}
          description={empty.description}
          action={empty.action}
        />
      );
    }
    return <>{empty}</>;
  }
  return <>{children}</>;
}

function isEmptyConfig(
  empty: EmptyConfig,
): empty is Extract<EmptyConfig, { heading: string }> {
  return (
    typeof empty === "object" &&
    empty != null &&
    "heading" in empty &&
    typeof (empty as { heading?: unknown }).heading === "string"
  );
}
