import * as React from "react";

import { cn } from "@/lib/utils";
import { Layout } from "./layout";

/**
 * The layout for pages about one thing: an MCP server, a policy, a
 * deployment, a chat session. Header band, optional tab strip, a main
 * column, and an optional aside for metadata (a `DetailList` belongs
 * there).
 *
 *   <DetailLayout>
 *     <DetailLayout.Header eyebrow="MCP Server" title={server.name} actions={…} />
 *     <DetailLayout.Tabs>{tabStrip}</DetailLayout.Tabs>
 *     <DetailLayout.Content>
 *       <DetailLayout.Main>{sections}</DetailLayout.Main>
 *       <DetailLayout.Aside><DetailList … /></DetailLayout.Aside>
 *     </DetailLayout.Content>
 *   </DetailLayout>
 */
function DetailLayoutRoot({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return <Layout className={className}>{children}</Layout>;
}

/** Sub-navigation directly under the header band. */
function DetailLayoutTabs({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("border-neutral-softest -mt-px border-b", className)}>
      {children}
    </div>
  );
}

/** Two-column wrapper. With no `Aside`, `Main` fills the width. */
function DetailLayoutContent({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <Layout.Body
      className={cn("gap-8 lg:flex-row lg:items-start lg:gap-12", className)}
    >
      {children}
    </Layout.Body>
  );
}

function DetailLayoutMain({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex min-w-0 flex-1 flex-col gap-8", className)}>
      {children}
    </div>
  );
}

/** Metadata column: identifiers, timestamps, owners, quick facts. */
function DetailLayoutAside({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <aside
      className={cn("flex w-full shrink-0 flex-col gap-6 lg:w-72", className)}
    >
      {children}
    </aside>
  );
}

DetailLayoutRoot.Header = Layout.Header;
DetailLayoutRoot.Tabs = DetailLayoutTabs;
DetailLayoutRoot.Content = DetailLayoutContent;
DetailLayoutRoot.Main = DetailLayoutMain;
DetailLayoutRoot.Aside = DetailLayoutAside;
DetailLayoutRoot.Section = Layout.Section;
DetailLayoutRoot.Actions = Layout.Actions;

export { DetailLayoutRoot as DetailLayout };
