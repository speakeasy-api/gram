// oxlint-disable react/only-export-components -- compound component (Object.assign) pattern
import { cn } from "@/lib/utils";
import React from "react";
import { MoreActions } from "./more-actions";
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

export const MiniCard = Object.assign(MiniCardComponent, {
  Title: MiniCardTitle,
  Description: MiniCardDescription,
  Actions: MoreActions,
});
