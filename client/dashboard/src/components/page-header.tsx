import { Separator } from "@/components/ui/separator";
import { SidebarTrigger } from "@/components/ui/sidebar";
import { Heading } from "./ui/heading.tsx";
import { Link, useParams, useLocation } from "react-router-dom";
import React from "react";

function PageHeaderComponent({ children }: { children: React.ReactNode }) {
  return (
    <header className="flex h-(--header-height) shrink-0 items-center gap-2 border-b transition-[width,height] ease-linear group-has-data-[collapsible=icon]/sidebar-wrapper:h-(--header-height)">
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

function PageHeaderTitle({ children }: { children: React.ReactNode }) {
  return (
    <Heading variant="h4" className="ml-1">
      {children}
    </Heading>
  );
}

function PageHeaderActions({ children }: { children: React.ReactNode }) {
  return <div className="ml-auto flex items-center">{children}</div>;
}

function PageHeaderBreadcrumbs() {
  const location = useLocation();

  // Build breadcrumb elements from URL segments
  const visibleElements = location.pathname
    .split("/")
    .filter(Boolean) // Remove empty strings
    .map((segment, index, segments) => {
      const url = "/" + segments.slice(0, index + 1).join("/");
      const isCurrentPage = url === location.pathname;

      return {
        url,
        title: segment,
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
                to={elem.url}
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
