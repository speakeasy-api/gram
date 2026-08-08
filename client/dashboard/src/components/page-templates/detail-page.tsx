import { DetailBody } from "@/components/detail-body";
import { Link } from "react-router";
import { Button } from "@/components/ui/Button";
import { Heading } from "@/components/ui/Heading";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import type { ReleaseStage } from "@/components/release-stage-badge";
import type { ReactNode } from "react";
import { TabbedPage, type PageTab } from "./tabbed-page";
import {
  TemplateFrame,
  TemplateHeader,
  type TemplateFrameProps,
  type TemplateHeaderProps,
} from "./scaffold";

export interface DetailSection {
  id: string;
  label: string;
  /** Internal route the rail/tab links to (required for `routed` layout). */
  href?: string;
  content: ReactNode;
  stage?: ReleaseStage;
}

/**
 * DetailPage — one entity, rendered as a hero + a set of sections. Generalizes
 * the mcp + plugins pattern (canonical), with skills' hash-scroll layout folded
 * in as a variant. The "at a glance" rail lives in the app sidebar and is wired
 * separately (see the DetailSidebarNav shell); this template owns the frame,
 * the hero, and the section body.
 *
 * `layout`:
 *  - "routed" (default): one section at a time, selected by URL path — a tab
 *    strip drives it. This is the mcp/plugins model.
 *  - "scroll": every section stacked in one column.
 *  - "hash-scroll": every section stacked, each anchored by `id` for hash nav
 *    (the skills variant — prefer "routed" for new pages).
 */
export function DetailPage({
  scope,
  scopeAll,
  resourceId,
  breadcrumbSubstitutions,
  hero,
  title,
  description,
  stage,
  area,
  sections,
  activeSection,
  layout = "routed",
  loading = false,
  notFound,
}: TemplateFrameProps &
  Partial<TemplateHeaderProps> & {
    hero?: ReactNode;
    sections: DetailSection[];
    /** Active section id for `routed` layout. */
    activeSection?: string;
    layout?: "routed" | "scroll" | "hash-scroll";
    loading?: boolean;
    notFound?: { title: string; description?: string; backTo?: string };
  }): JSX.Element {
  if (loading) {
    return (
      <TemplateFrame
        scope={scope}
        breadcrumbSubstitutions={breadcrumbSubstitutions}
        fullWidth
        fullWidthBreadcrumbs
        bodyClassName="gap-0"
      >
        <DetailBody>
          <SkeletonTable />
        </DetailBody>
      </TemplateFrame>
    );
  }

  if (notFound != null) {
    return (
      <TemplateFrame
        scope={scope}
        breadcrumbSubstitutions={breadcrumbSubstitutions}
        fullWidth
        fullWidthBreadcrumbs
        bodyClassName="gap-0"
      >
        <DetailBody>
          <div className="flex flex-col items-center gap-3 border border-dashed px-8 py-16 text-center">
            <Heading variant="h5" className="font-medium">
              {notFound.title}
            </Heading>
            {notFound.description != null && (
              <Text small muted className="max-w-md">
                {notFound.description}
              </Text>
            )}
            {notFound.backTo != null && (
              <Link to={notFound.backTo}>
                <Button variant="tertiary" size="sm">
                  Go back
                </Button>
              </Link>
            )}
          </div>
        </DetailBody>
      </TemplateFrame>
    );
  }

  if (layout === "routed") {
    const tabs: PageTab[] = sections.map((section) => ({
      value: section.id,
      label: section.label,
      href: section.href ?? "#",
      stage: section.stage,
    }));
    const active =
      sections.find((section) => section.id === activeSection) ?? sections[0];
    return (
      <TabbedPage
        scope={scope}
        scopeAll={scopeAll}
        resourceId={resourceId}
        breadcrumbSubstitutions={breadcrumbSubstitutions}
        hero={hero}
        title={title}
        description={description}
        stage={stage}
        area={area}
        tabs={tabs}
        activeTab={active?.id ?? ""}
      >
        {active?.content}
      </TabbedPage>
    );
  }

  // scroll / hash-scroll: every section stacked in one column.
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
            />
          </div>
        ))}
      <DetailBody spacing="loose" fill>
        {sections.map((section) => (
          <section key={section.id} id={section.id}>
            {section.content}
          </section>
        ))}
      </DetailBody>
    </TemplateFrame>
  );
}
