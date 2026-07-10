import * as React from "react";

import { Toolbar } from "@/components/ui/toolbar";
import { cn } from "@/lib/utils";
import { Layout } from "./layout";

/**
 * The layout for pages whose job is "find one of these": MCP servers,
 * sources, collections, API keys, audit logs. A header band, the standard
 * control bar, the collection, and an optional footer.
 *
 *   <ListLayout>
 *     <ListLayout.Header title="MCP Servers" subtitle="…" actions={<Button/>} />
 *     <ListLayout.Toolbar>
 *       <ListLayout.Toolbar.Search … />
 *       <ListLayout.Toolbar.Count … />
 *     </ListLayout.Toolbar>
 *     <ListLayout.List>{table or cards}</ListLayout.List>
 *     <ListLayout.Footer><LoadMoreFooter … /></ListLayout.Footer>
 *   </ListLayout>
 */
function ListLayoutRoot({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}): JSX.Element {
  return <Layout className={className}>{children}</Layout>;
}

/** The collection itself: a table, a card grid, a feed. */
function ListLayoutList({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}): JSX.Element {
  return (
    <Layout.Body className={cn("gap-4", className)}>{children}</Layout.Body>
  );
}

/** Pagination, load-more, or result summary beneath the collection. */
function ListLayoutFooter({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}): JSX.Element {
  return <div className={cn("mt-2", className)}>{children}</div>;
}

ListLayoutRoot.Header = Layout.Header;
ListLayoutRoot.Toolbar = Toolbar;
ListLayoutRoot.List = ListLayoutList;
ListLayoutRoot.Footer = ListLayoutFooter;
ListLayoutRoot.Actions = Layout.Actions;

export { ListLayoutRoot as ListLayout };
