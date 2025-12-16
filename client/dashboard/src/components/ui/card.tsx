import * as React from "react";

import { cn } from "@/lib/utils";
import { Grid } from "@speakeasy-api/moonshine";
import { Heading } from "./heading";
import { Skeleton, SkeletonParagraph } from "./skeleton";
import { Type } from "./type";

const CardComponent = ({
  className,
  size = "default",
  ...props
}: React.ComponentProps<"div"> & { size?: "default" | "sm" }) => {
  return (
    <div
      data-slot="card"
      className={cn(
        "bg-card text-card-foreground flex flex-col gap-5 rounded-xl border p-4 group/card",
        size === "sm" && "gap-4 py-4",
        className,
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
        "@container/card-header flex items-center justify-between [.border-b]:pb-6",
        className,
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
      className={cn(
        "leading-none [a:has([data-slot=card]):hover_&]:underline normal-case", // Underline the title when the card is hovered, if the card is a link
        className,
      )}
      {...props}
    />
  );
}

function CardDescription({
  className,
  ...props
}: React.ComponentProps<typeof Type>) {
  return (
    <Type
      muted
      small
      data-slot="card-description"
      className={cn("w-full truncate", className)}
      {...props}
    />
  );
}

function CardInfo({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-info"
      className={cn(
        "gap-2 flex justify-start ml-auto",
        "group-hover/card:has([data-slot=card-action]):opacity-0", // Only hide info when card has an action and is hovered
        className,
      )}
      {...props}
    />
  );
}

function CardActions({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div data-slot="card-action" className={cn("flex", className)} {...props} />
  );
}

function CardContent({ className, ...props }: React.ComponentProps<"div">) {
  return <div data-slot="card-content" className={className} {...props} />;
}

function CardFooter({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-footer"
      className={cn(
        "mt-auto flex items-center justify-between [.border-t]:pt-6 w-full",
        className,
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
  isLoading: loading,
  noGrid,
  cardSize = 2,
  ...props
}: React.ComponentProps<"div"> & {
  isLoading?: boolean;
  noGrid?: boolean;
  cardSize?: number;
}) {
  let children = React.Children.map(props.children, (child) => (
    <Grid.Item colSpan={cardSize}>{child}</Grid.Item>
  ));

  if (loading) {
    children = [
      <Grid.Item key="one" colSpan={cardSize}>
        <CardSkeleton />
      </Grid.Item>,
      <Grid.Item key="two" colSpan={cardSize}>
        <CardSkeleton />
      </Grid.Item>,
      <Grid.Item key="three" colSpan={cardSize}>
        <CardSkeleton />
      </Grid.Item>,
    ];
  }

  if (!children) {
    return "Nothing found";
  }

  return (
    <div className="@container/cards">
      <Grid
        columns={1}
        className={cn(
          "grid-cols-1 gap-x-8 gap-y-4 mb-8",
          !noGrid &&
            "@lg/cards:grid-cols-2 @3xl/cards:grid-cols-4 @7xl/cards:grid-cols-6",
          className,
        )}
        {...props}
      >
        {children}
      </Grid>
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
