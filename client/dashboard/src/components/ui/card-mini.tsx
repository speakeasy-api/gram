import { cn } from "@/lib/utils";
import React from "react";
import { Cards } from "./card";
import { MoreActions } from "./more-actions";
import { Skeleton } from "./skeleton";
import { Type } from "./type";

const MiniCardComponent = ({
  className,
  size = "default",
  ...props
}: React.ComponentProps<"div"> & { size?: "default" | "sm" }) => {
  const slots = {
    title: null as React.ReactElement | null,
    description: null as React.ReactElement | null,
    actions: null as React.ReactElement | null,
  };

  const otherChildren: React.ReactNode[] = [];

  React.Children.forEach(props.children, (child) => {
    if (React.isValidElement(child)) {
      if (child.type === MiniCardTitle) {
        slots.title = child;
      } else if (child.type === MiniCardDescription) {
        slots.description = child;
      } else if (child.type === MiniCard.Actions) {
        slots.actions = child;
      } else {
        otherChildren.push(child);
      }
    }
  });

  return (
    <div
      data-slot="card"
      className={cn(
        "bg-card text-card-foreground group/card flex max-h-fit max-w-sm items-center justify-between rounded-md border px-3 py-4",
        size === "sm" && "gap-4 py-4",
        className,
      )}
      {...props}
    >
      <div data-slot="card-content" className={"flex w-full flex-col gap-1.5"}>
        {slots.title}
        {slots.description}
      </div>
      {slots.actions}
      {otherChildren}
    </div>
  );
};

function MiniCardTitle({
  className,
  ...props
}: React.ComponentProps<typeof Type>) {
  return (
    <Type
      data-slot="card-title"
      className={cn("text-foreground! leading-none font-normal", className)}
      {...props}
    />
  );
}

function MiniCardDescription({
  className,
  ...props
}: React.ComponentProps<typeof Type>) {
  return (
    <Type
      muted
      data-slot="card-description"
      className={cn("w-full truncate text-xs", className)}
      {...props}
    />
  );
}

export function MiniCards({
  className,
  children,
  isLoading,
}: {
  className?: string;
  children: React.ReactNode;
  isLoading?: boolean;
}) {
  let content = children;
  if (isLoading) {
    content = (
      <>
        <MiniCardSkeleton />
        <MiniCardSkeleton />
        <MiniCardSkeleton />
      </>
    );
  }

  return (
    <Cards cardSize={1} className={cn("gap-x-3 gap-y-2", className)}>
      {content}
    </Cards>
  );
}

function MiniCardSkeleton() {
  return (
    <MiniCard>
      <MiniCard.Title>
        <Skeleton className="h-4 w-[150px]" />
      </MiniCard.Title>
      <MiniCard.Description>
        <Skeleton className="h-4 w-full" />
      </MiniCard.Description>
    </MiniCard>
  );
}

export const MiniCard = Object.assign(MiniCardComponent, {
  Title: MiniCardTitle,
  Description: MiniCardDescription,
  Actions: MoreActions,
});
