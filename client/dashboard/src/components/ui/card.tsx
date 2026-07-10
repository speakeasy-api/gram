import * as React from "react";

import { cn } from "@/lib/utils";
import { Grid } from "@/components/ui/moonshine";
import { Heading } from "./heading";
import { Skeleton, SkeletonParagraph } from "./skeleton";
import { Type } from "./type";

/**
 * The design-system card: hairline border on a paper surface, squared
 * corners, no shadows — hover darkens the border. Passing `icon` (and
 * optionally `overlay`) adds the signature dot-pattern sidebar on the
 * left, the visual carried over from the legacy DotCard.
 */
function Card({
  className,
  size = "default",
  icon,
  overlay,
  children,
  ...props
}: React.ComponentProps<"div"> & {
  size?: "default" | "sm";
  /** Content centered in a frosted container over the dot-pattern sidebar */
  icon?: React.ReactNode;
  /** Extra content layered on the dot sidebar (e.g. an "Added" badge) */
  overlay?: React.ReactNode;
}): React.JSX.Element {
  const content = (
    <div
      data-slot="card-body"
      className={cn(
        "flex min-w-0 flex-1 flex-col gap-5 p-4",
        size === "sm" && "gap-4 py-4",
      )}
    >
      {children}
    </div>
  );

  return (
    <div
      data-slot="card"
      className={cn(
        "bg-card text-card-foreground group/card border-neutral-softest flex border transition-colors",
        "hover:border-neutral-default",
        icon
          ? "h-full min-h-[156px] flex-row overflow-hidden"
          : cn("flex-col gap-5 p-4", size === "sm" && "gap-4 py-4"),
        className,
      )}
      {...props}
    >
      {icon && (
        <div className="bg-muted/30 text-muted-foreground/20 relative w-40 shrink-0 overflow-hidden border-r">
          <div
            className="scroll-dots-target absolute inset-0"
            style={{
              backgroundImage:
                "radial-gradient(circle, currentColor 1px, transparent 1px)",
              backgroundSize: "16px 16px",
            }}
          />
          <div className="absolute inset-0 flex items-center justify-center">
            <div className="bg-background/90 p-3 backdrop-blur-sm dark:bg-neutral-800 dark:backdrop-blur-none">
              {icon}
            </div>
          </div>
          {overlay}
        </div>
      )}
      {icon ? content : children}
    </div>
  );
}

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
        "leading-none normal-case [a:has([data-slot=card]):hover_&]:underline", // Underline the title when the card is hovered, if the card is a link
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
        "ml-auto flex justify-start gap-2",
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
        "mt-auto flex w-full items-center justify-between [.border-t]:pt-6",
        className,
      )}
      {...props}
    />
  );
}

// Compound members are attached by mutation rather than Object.assign: the
// react/only-export-components rule recognizes the former as a component
// export and flags the latter.
Card.Header = CardHeader;
Card.Title = CardTitle;
Card.Description = CardDescription;
Card.Info = CardInfo;
Card.Actions = CardActions;
Card.Content = CardContent;
Card.Footer = CardFooter;

export { Card };

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
}): React.JSX.Element {
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
    return <>Nothing found</>;
  }

  return (
    <div className="@container/cards">
      <Grid
        columns={1}
        className={cn(
          "mb-8 grid-cols-1 gap-x-8 gap-y-4",
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

export function CardSkeleton(): React.JSX.Element {
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
