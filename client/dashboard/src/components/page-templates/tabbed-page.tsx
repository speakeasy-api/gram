import { DetailBody } from "@/components/detail-body";
import { Page } from "@/components/page-layout";
import {
  ReleaseStageBadge,
  type ReleaseStage,
} from "@/components/release-stage-badge";
import { PageTabsList, PageTabsTrigger, Tabs } from "@/components/ui/Tabs";
import type { ReactNode } from "react";
import { Link } from "react-router";
import {
  TemplateFrame,
  TemplateHeader,
  type TemplateFrameProps,
  type TemplateHeaderProps,
} from "./scaffold";

export interface PageTab {
  /** Stable value; must match `activeTab`. */
  value: string;
  label: string;
  /** Internal route the tab links to (path-based, one tab at a time). */
  href: string;
  stage?: ReleaseStage;
}

/**
 * TabbedPage — the shared tab-strip shell for pages whose sub-views are
 * separate routed subpages (tabs are *different resources*, e.g. Access →
 * roles/members, or PolicyCenter → exclusions/dismissed). DetailPage builds on
 * this by adding an entity hero.
 *
 * Path-based: the caller renders the active subpage as `children`; the strip is
 * driven by `activeTab` and each tab is a `<Link>`.
 *
 *   <TabbedPage
 *     title="Access"
 *     tabs={[
 *       { value: "roles", label: "Roles", href: rolesHref },
 *       { value: "members", label: "Members", href: membersHref },
 *     ]}
 *     activeTab={activeTab}
 *   >
 *     {activeTab === "roles" ? <RolesTab /> : <MembersTab />}
 *   </TabbedPage>
 */
export function TabbedPage({
  scope,
  scopeAll,
  resourceId,
  breadcrumbSubstitutions,
  title,
  description,
  stage,
  area,
  primaryAction,
  hero,
  tabs,
  activeTab,
  children,
}: TemplateFrameProps &
  Partial<TemplateHeaderProps> & {
    /** Custom hero (e.g. a DetailHero) rendered in place of the plain header. */
    hero?: ReactNode;
    tabs: PageTab[];
    activeTab: string;
    children: ReactNode;
  }): JSX.Element {
  return (
    <TemplateFrame
      scope={scope}
      scopeAll={scopeAll}
      resourceId={resourceId}
      breadcrumbSubstitutions={breadcrumbSubstitutions}
      fullWidth
      fullWidthBreadcrumbs
      bodyClassName="gap-0"
    >
      {hero ??
        (title != null && (
          <div className="mx-auto w-full max-w-[1270px] px-8">
            <TemplateHeader
              title={title}
              description={description}
              stage={stage}
              area={area}
              primaryAction={primaryAction}
            />
          </div>
        ))}

      <Tabs value={activeTab} className="flex w-full flex-1 flex-col">
        <div className="shrink-0 border-b">
          <div className="mx-auto max-w-[1270px] px-8">
            <PageTabsList className="h-auto gap-6 bg-transparent p-0">
              {tabs.map((tab) => (
                <PageTabsTrigger key={tab.value} value={tab.value} asChild>
                  <Link
                    to={tab.href}
                    className="inline-flex items-center gap-2"
                  >
                    {tab.label}
                    {tab.stage != null && (
                      <ReleaseStageBadge stage={tab.stage} noTooltip />
                    )}
                  </Link>
                </PageTabsTrigger>
              ))}
            </PageTabsList>
          </div>
        </div>
        <DetailBody fill>{children}</DetailBody>
      </Tabs>
    </TemplateFrame>
  );
}

// Re-exported so callers can build the Page shell without also importing Page.
export { Page };
