import { cn } from "@/lib/utils";
import { Stack } from "@speakeasy-api/moonshine";
import React from "react";
import { MoreActions } from "./more-actions";
import { Skeleton } from "./skeleton";
import { Type } from "./type";

// function PageSectionComponent({ children }: { children: React.ReactNode }) {
//   const slots = {
//     title: null as React.ReactElement | null,
//     description: null as React.ReactElement | null,
//     cta: null as React.ReactElement | null,
//     body: null as React.ReactElement | null,
//   };

//   // Process children to extract slots by checking component type
//   React.Children.forEach(children, (child) => {
//     if (React.isValidElement(child)) {
//       // Check if the child is one of our slot components
//       if (child.type === PageSectionTitle) {
//         slots.title = child;
//       } else if (child.type === PageSectionDescription) {
//         slots.description = child;
//       } else if (child.type === PageSectionCTA) {
//         slots.cta = child;
//       } else if (child.type === PageSectionBody) {
//         slots.body = child;
//       }
//     }
//   });

//   return (
//     <Stack gap={2} className="mb-8">
//       {/* Render header with title, description, and CTA if they exist */}
//       {(slots.title || slots.description || slots.cta) && (
//         <Stack
//           direction="horizontal"
//           justify="space-between"
//           align="center"
//           className="mb-4"
//         >
//           <Stack gap={2}>
//             {slots.title}
//             {slots.description}
//           </Stack>
//           {slots.cta}
//         </Stack>
//       )}
//       {/* Render body */}
//       {slots.body}
//     </Stack>
//   );
// }

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
        "bg-card min-w-2xs max-w-sm max-h-fit text-card-foreground flex justify-between items-center rounded-md border px-3 py-4 group/card",
        size === "sm" && "gap-4 py-4",
        className
      )}
      {...props}
    >
      <div data-slot="card-content" className={"flex flex-col gap-1.5 w-full"}>
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
      className={cn("leading-none font-normal text-foreground!", className)}
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
    <Stack direction="horizontal" gap={3} className={className} wrap="wrap">
      {content}
    </Stack>
  );
}

function MiniCardSkeleton() {
  return (
    <MiniCard>
      <MiniCard.Title>
        <Skeleton className="w-[150px] h-4" />
      </MiniCard.Title>
      <MiniCard.Description>
        <Skeleton className="w-full h-4" />
      </MiniCard.Description>
    </MiniCard>
  );
}

export const MiniCard = Object.assign(MiniCardComponent, {
  Title: MiniCardTitle,
  Description: MiniCardDescription,
  Actions: MoreActions,
});
