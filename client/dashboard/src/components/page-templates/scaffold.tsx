// oxlint-disable react/only-export-components -- shared template building blocks
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import type { ReleaseStage } from "@/components/release-stage-badge";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import type { ReactNode } from "react";

/**
 * Shared plumbing for the page templates. These are NOT rendered directly by
 * pages — pick a concrete template (ResourceListPage, DetailPage, …). They
 * exist so every template opens the same way: the `Page` frame, breadcrumbs,
 * a scope gate, and a header defined exactly once (so loading / empty / error
 * states are props, never parallel header copies — the "three-branch header"
 * trap).
 */

/** Props every template accepts for the page frame + scope gate. */
export interface TemplateFrameProps {
  /** Scope(s) required to view the page. Omit for an ungated page. */
  scope?: Scope | Scope[];
  /** When true, ALL scopes are required (default: any). */
  scopeAll?: boolean;
  /** Resource id to check scopes against. */
  resourceId?: string;
  /** Breadcrumb label overrides, e.g. `{ [slug]: entity?.name }`. */
  breadcrumbSubstitutions?: Record<string, string | undefined>;
}

/** Props for the single, canonical page header. */
export interface TemplateHeaderProps {
  title: ReactNode;
  description?: ReactNode;
  /** Preview / Beta badge on the primary title. */
  stage?: ReleaseStage;
  /** Override the auto-derived area eyebrow; "" suppresses it. */
  area?: string;
  /** Right-aligned primary CTA(s) beside the title. */
  primaryAction?: ReactNode;
}

/**
 * The Page frame + breadcrumbs + optional scope gate. Body layout flags mirror
 * `Page.Body` so fullbleed templates (Workbench) can opt in.
 */
export function TemplateFrame({
  scope,
  scopeAll,
  resourceId,
  breadcrumbSubstitutions,
  fullWidth,
  fullHeight,
  noPadding,
  overflowHidden,
  bodyClassName,
  fullWidthBreadcrumbs,
  children,
}: TemplateFrameProps & {
  fullWidth?: boolean;
  fullHeight?: boolean;
  noPadding?: boolean;
  overflowHidden?: boolean;
  bodyClassName?: string;
  fullWidthBreadcrumbs?: boolean;
  children: ReactNode;
}): JSX.Element {
  const gated =
    scope != null ? (
      <RequireScope
        scope={scope}
        all={scopeAll}
        resourceId={resourceId}
        level="page"
      >
        {children}
      </RequireScope>
    ) : (
      children
    );

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          fullWidth={fullWidthBreadcrumbs}
          substitutions={breadcrumbSubstitutions}
        />
      </Page.Header>
      <Page.Body
        fullWidth={fullWidth}
        fullHeight={fullHeight}
        noPadding={noPadding}
        overflowHidden={overflowHidden}
        className={bodyClassName}
      >
        {gated}
      </Page.Body>
    </Page>
  );
}

/**
 * The canonical page header: area eyebrow + thin serif title + description,
 * with right-aligned actions. Rendered once per template.
 */
export function TemplateHeader({
  title,
  description,
  stage,
  area,
  primaryAction,
}: TemplateHeaderProps): JSX.Element {
  return (
    <Page.Section>
      <Page.Section.Title stage={stage} area={area}>
        {title}
      </Page.Section.Title>
      {description != null ? (
        <Page.Section.Description>{description}</Page.Section.Description>
      ) : null}
      {primaryAction != null ? (
        <Page.Section.CTA>{primaryAction}</Page.Section.CTA>
      ) : null}
    </Page.Section>
  );
}
