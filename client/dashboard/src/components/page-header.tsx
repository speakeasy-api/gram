import { Separator } from "@/components/ui/separator";
import { SidebarTrigger } from "@/components/ui/sidebar";
import { useSlugs } from "@/contexts/Sdk.tsx";
import { capitalize, cn } from "@/lib/utils.ts";
import React from "react";
import { Link, useLocation, useParams } from "react-router";
import { Heading } from "./ui/heading.tsx";

function PageHeaderComponent({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <header
      className={cn(
        "flex h-(--header-height) shrink-0 items-center gap-2 border-b transition-[width,height] ease-linear group-has-data-[collapsible=icon]/sidebar-wrapper:h-(--header-height)",
        className,
      )}
    >
      <div className="flex w-full items-center px-3 gap-3">
        <SidebarTrigger className="-ml-1 mx-0 px-0" />
        <Separator
          orientation="vertical"
          className="data-[orientation=vertical]:h-4"
        />
        {children}
      </div>
    </header>
  );
}

function PageHeaderTitle({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}) {
  return (
    // 1270 carefully chosen to make the header line up with the max width of the page content
    <Heading
      variant="h4"
      className={cn("ml-1 max-w-[1270px] w-full mx-auto", className)}
    >
      {children}
    </Heading>
  );
}

const breadcrumbSubstitutions = {
  mcp: "MCP",
  sdks: "SDKs",
  "custom-tools": "Custom Tools",
};

function PageHeaderBreadcrumbs({
  fullWidth,
  className,
  substitutions = {}, // Any segment and how it should be displayed, for example toolset slug -> toolset name
  skipSegments = [], // Segments to skip/hide from breadcrumbs
}: {
  fullWidth?: boolean;
  className?: string;
  substitutions?: Record<string, string | undefined>;
  skipSegments?: string[];
}) {
  const params = useParams();
  const { orgSlug, projectSlug } = useSlugs();
  const location = useLocation();

  const toPreserve = Object.values(params).filter(Boolean);
  const allSubstitutions: Record<string, string | undefined> = {
    ...breadcrumbSubstitutions,
    ...substitutions,
  };

  // Build breadcrumb elements from URL segments
  const allSegments = location.pathname
    .split("/")
    .filter(Boolean)
    .slice(2);

  const visibleElements = allSegments
    .map((segment, index) => {
      const url = "/" + allSegments.slice(0, index + 1).join("/");
      const isCurrentPage = index === allSegments.length - 1;

      if (skipSegments.includes(segment)) {
        return null;
      }

      let display = segment;
      if (allSubstitutions[segment]) {
        display = allSubstitutions[segment];
      } else if (!toPreserve.includes(segment)) {
        display = capitalize(segment);
      }

      return {
        url,
        display,
        isCurrentPage,
      };
    })
    .filter((elem): elem is NonNullable<typeof elem> => elem !== null);

  visibleElements.unshift({
    url: "/",
    display: "Home",
    isCurrentPage: visibleElements.length === 0,
  });

  return (
    <PageHeader.Title className={cn(fullWidth ? "max-w-full" : "", className)}>
      <div className="ml-auto flex items-center gap-2 normal-case">
        {visibleElements.map((elem, index) => (
          <React.Fragment key={elem.url}>
            {elem.isCurrentPage ? (
              <span>{elem.display}</span>
            ) : (
              <Link
                to={`/${orgSlug}/${projectSlug}${elem.url}`}
                className="text-muted-foreground hover:text-foreground trans"
              >
                {elem.display}
              </Link>
            )}
            {index < visibleElements.length - 1 && (
              <span className="text-muted-foreground"> / </span>
            )}
          </React.Fragment>
        ))}
      </div>
    </PageHeader.Title>
  );
}

export const PageHeader = Object.assign(PageHeaderComponent, {
  Title: PageHeaderTitle,
  Breadcrumbs: PageHeaderBreadcrumbs,
});
