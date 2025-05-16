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
        className
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
    <Heading variant="h4" className={cn("ml-1", className)}>
      {children}
    </Heading>
  );
}

function PageHeaderActions({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <div className={cn("ml-auto flex items-center", className)}>{children}</div>
  );
}

function PageHeaderBreadcrumbs() {
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
      const title = segment.toLowerCase() === "sdk" ? "SDK" : segment.replace("-", " ");

      return {
        url,
        title,
        isCurrentPage,
      };
    });

  if (visibleElements.length === 0) {
    visibleElements.push({
      url: "/",
      title: "Home",
      isCurrentPage: location.pathname === "/",
    });
  }

  return (
    <PageHeader.Title>
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
  Actions: PageHeaderActions,
  Breadcrumbs: PageHeaderBreadcrumbs,
});
