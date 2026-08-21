import * as TabsPrimitive from "@radix-ui/react-tabs";
import * as React from "react";

import { cn } from "@/lib/utils";

function Tabs({
  className,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.Root>): React.JSX.Element {
  return (
    <TabsPrimitive.Root
      data-slot="tabs"
      className={cn("flex flex-col gap-2", className)}
      {...props}
    />
  );
}

function TabsList({
  className,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.List>): React.JSX.Element {
  return (
    <TabsPrimitive.List
      data-slot="tabs-list"
      className={cn(
        "text-muted-foreground border-border bg-card divide-border inline-flex h-9 w-fit items-center justify-center divide-x border",
        className,
      )}
      {...props}
    />
  );
}

function TabsTrigger({
  className,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.Trigger>): React.JSX.Element {
  return (
    <TabsPrimitive.Trigger
      data-slot="tabs-trigger"
      className={cn(
        "data-[state=active]:bg-primary data-[state=active]:text-primary-foreground focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:outline-ring text-muted-foreground data-[state=inactive]:hover:text-foreground inline-flex h-full flex-1 items-center justify-center gap-1.5 px-3 py-1 font-mono text-xs tracking-[0.08em] uppercase whitespace-nowrap transition-[color,box-shadow] focus-visible:ring-[3px] focus-visible:outline-1 disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
        className,
      )}
      {...props}
    />
  );
}

function TabsContent({
  className,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.Content>): React.JSX.Element {
  return (
    <TabsPrimitive.Content
      data-slot="tabs-content"
      className={cn("flex-1 outline-none", className)}
      {...props}
    />
  );
}

/** Underline-style tab list for page-level detail views; hosts PageTabsTrigger. */
function PageTabsList({
  className,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.List>): React.JSX.Element {
  return (
    <TabsList
      className={cn(
        // Flush page-level tabs: undo the boxed track TabsList draws
        // (border + dividers) — page tabs are underline-style, not segments.
        "h-auto justify-start gap-4 divide-x-0 border-0 bg-transparent p-0 text-sm",
        className,
      )}
      {...props}
    />
  );
}

/** Underline-style tab trigger for page-level detail views. */
function PageTabsTrigger({
  className,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.Trigger>): React.JSX.Element {
  return (
    <TabsPrimitive.Trigger
      data-slot="tabs-trigger"
      className={cn(
        "text-muted-foreground data-[state=active]:text-foreground data-[state=active]:after:bg-primary relative h-11 border-none bg-transparent! px-1 pt-3 pb-3 font-mono text-xs tracking-[0.08em] uppercase shadow-none! after:absolute after:right-0 after:bottom-0 after:left-0 after:h-0.5 after:bg-transparent data-[state=active]:bg-transparent!",
        className,
      )}
      {...props}
    />
  );
}

export {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  PageTabsList,
  PageTabsTrigger,
};
