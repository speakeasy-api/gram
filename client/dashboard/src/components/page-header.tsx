import { Separator } from "@/components/ui/separator";
import { SidebarTrigger } from "@/components/ui/sidebar";
import { useSlugs } from "@/contexts/Sdk.tsx";
import { cn } from "@/lib/utils.ts";
import React from "react";
import { Link, useLocation } from "react-router";
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

function PageHeaderBreadcrumbs({
  fullWidth,
  className,
}: {
  fullWidth?: boolean;
  className?: string;
}) {
  const { orgSlug, projectSlug } = useSlugs();
  const location = useLocation();

  // Build breadcrumb elements from URL segments
  const visibleElements = location.pathname
    .split("/")
    .filter(Boolean) // Remove empty strings
    .slice(2) // Remove the two leading elements (org slug and project slug)
    .map((segment, index, segments) => {
      const url = "/" + segments.slice(0, index + 1).join("/");
      const isCurrentPage = location.pathname.endsWith(url);
      let title = segment.replace("-", " ");
      if (title.toLowerCase() === "sdks") {
        title = "SDKs";
      }
      if (title.toLowerCase() === "mcp") {
        title = "MCP";
      }

      return {
        url,
        title,
        isCurrentPage,
      };
    });

  visibleElements.unshift({
    url: "/",
    title: "Home",
    isCurrentPage: visibleElements.length === 0,
  });

  return (
    <PageHeader.Title className={cn(fullWidth ? "max-w-full" : "", className)}>
      <div className="ml-auto flex items-center gap-2 capitalize">
        {visibleElements.map((elem, index) => (
          <React.Fragment key={elem.url}>
            {elem.isCurrentPage ? (
              <span>{elem.title}</span>
            ) : (
              <Link
                to={`/${orgSlug}/${projectSlug}${elem.url}`}
                className="text-muted-foreground hover:text-foreground trans"
              >
                {elem.title}
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
