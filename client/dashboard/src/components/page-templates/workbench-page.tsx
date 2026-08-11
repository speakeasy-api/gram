import type { ReactNode } from "react";
import { TemplateFrame, type TemplateFrameProps } from "./scaffold";

/**
 * WorkbenchPage — a fullbleed analytics/observe surface that owns its own
 * internal layout: a sticky filter/date bar, a big virtualized table or charts,
 * and internal scroll. This is the shape behind Tool Logs, Risk Events, the
 * Insights pages, Costs, and Agent Logs — they all replicate the same
 * `Page.Body fullWidth overflowHidden noPadding` shell (today `ObservePageShell`).
 *
 * Unlike OverviewPage, the header/title is usually rendered by the workbench
 * body itself (next to its filter bar), so this template gives the frame and
 * an optional tab strip and gets out of the way.
 *
 *   <WorkbenchPage scope="observe:read" tabs={<ObserveTabNav base="logs" />}>
 *     <ToolLogsWorkbench />
 *   </WorkbenchPage>
 */
export function WorkbenchPage({
  scope,
  scopeAll,
  resourceId,
  breadcrumbSubstitutions,
  /** Optional tab strip rendered flush under the breadcrumbs. */
  tabs,
  children,
}: TemplateFrameProps & {
  tabs?: ReactNode;
  children: ReactNode;
}): JSX.Element {
  return (
    <TemplateFrame
      scope={scope}
      scopeAll={scopeAll}
      resourceId={resourceId}
      breadcrumbSubstitutions={breadcrumbSubstitutions}
      fullWidth
      fullHeight
      noPadding
      overflowHidden
      fullWidthBreadcrumbs
    >
      {tabs}
      {children}
    </TemplateFrame>
  );
}
