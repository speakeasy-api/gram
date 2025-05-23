import * as React from "react";

import { cn } from "@/lib/utils";
import { Heading } from "./heading";
import { Skeleton, SkeletonParagraph } from "./skeleton";

const CardComponent = ({
  className,
  size = "default",
  ...props
}: React.ComponentProps<"div"> & { size?: "default" | "sm" }) => {
  return (
    <div
      data-slot="card"
      className={cn(
        "bg-card max-w-2xl text-card-foreground flex flex-col gap-6 rounded-xl border py-6 shadow-sm group/card last:mb-8",
        size === "sm" && "gap-4 py-4",
        className
      )}
      {...props}
    />
  );
};

function CardHeader({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-header"
      className={cn(
        "@container/card-header grid auto-rows-min grid-rows-[auto_auto] items-start gap-2 px-6 [.border-b]:pb-6 relative",
        className
      )}
      {...props}
    />
  );
}

function CardTitle({
  className,
  ...props
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <Heading
      variant="h4"
      data-slot="card-title"
      className={cn("leading-none", className)}
      {...props}
    />
  );
}

function CardDescription({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-description"
      className={cn("text-muted-foreground text-sm", className)}
      {...props}
    />
  );
}

function CardInfo({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-info"
      className={cn(
        "absolute top-[-4px] right-6 bg-card trans gap-2 flex",
        ".group/card:has([data-slot=card-action]) group-hover/card:opacity-0", // If the card has an action, hide the info when hovering over the card
        className
      )}
      {...props}
    />
  );
}

function CardActions({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-action"
      className={cn(
        "absolute top-[-8px] right-4 bg-card opacity-0 group-hover/card:opacity-100 trans gap-2 flex",
        className
      )}
      {...props}
    />
  );
}

export function CardContent({
  className,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-content"
      className={cn("px-6", className)}
      {...props}
    />
  );
}

function CardFooter({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-footer"
      className={cn(
        "flex items-center justify-end gap-2 px-6 [.border-t]:pt-6",
        className
      )}
      {...props}
    />
  );
}

export const Card = Object.assign(CardComponent, {
  Header: CardHeader,
  Title: CardTitle,
  Description: CardDescription,
  Info: CardInfo,
  Actions: CardActions,
  Content: CardContent,
  Footer: CardFooter,
});

export function Cards({
  className,
  loading,
  ...props
}: React.ComponentProps<"div"> & { loading?: boolean }) {
  return (
    <div className={cn("flex flex-col gap-4", className)} {...props}>
      {loading ? (
        <>
          <CardSkeleton />
          <CardSkeleton />
          <CardSkeleton />
        </>
      ) : (
        props.children
      )}
    </div>
  );
}

export function CardSkeleton() {
  return (
    <Card>
      <Card.Header>
        <Card.Title>
          <Skeleton className="h-4 w-40" />
        </Card.Title>
        <Card.Description>
          <Skeleton className="h-4 w-full" />
        </Card.Description>
      </Card.Header>
      <Card.Content>
        <SkeletonParagraph />
      </Card.Content>
    </Card>
  );
}
