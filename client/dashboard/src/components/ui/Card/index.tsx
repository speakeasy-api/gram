import { cn } from "@/lib/utils";
import React, { FC, PropsWithChildren, ReactNode } from "react";
import { Icon } from "../Icon";
import { SimpleTooltip } from "../Tooltip";
import { Stack } from "../Stack";
import { Button } from "../Button";
import { Grid } from "../Grid";
import { Skeleton, SkeletonParagraph } from "../Skeleton";
import { iconNames } from "../Icon/names";
import { Children } from "react";

type RightElement = {
  type: "button";
  label: string;
  onClick: () => void;
};

type IconProps = {
  name: (typeof iconNames)[number];
  size?: "small" | "medium" | "large";
};

type CardHeaderProps = PropsWithChildren & {
  subheader?: React.ReactNode;
  icon?: IconProps;
  rightElement?: RightElement;
  className?: string;
};

const CardHeader: FC<CardHeaderProps> = ({
  children,
  subheader,
  icon,
  rightElement,
  className,
}) => (
  <div
    className={cn(
      "flex w-full flex-row gap-4",
      subheader ? "items-start" : "items-center",
      className,
    )}
  >
    {icon && (
      <div className="flex-shrink-0 border p-2">
        <Icon name={icon.name} size={icon.size} />
      </div>
    )}

    <div className="flex min-w-0 flex-grow flex-col gap-1">
      <div className="text-md leading-none font-semibold tracking-tight">
        {children}
      </div>
      {subheader && (
        <div className="mt-1 flex items-center text-sm text-card-foreground">
          {subheader}
        </div>
      )}
    </div>

    {rightElement && (
      <div className="flex flex-shrink-0 justify-end gap-2">
        {rightElement.type === "button" && (
          <Button onClick={rightElement.onClick} variant="secondary">
            {rightElement.label}
          </Button>
        )}
      </div>
    )}
  </div>
);
CardHeader.displayName = "CardHeader";

interface CardContentProps extends PropsWithChildren {
  className?: string;
}

const CardContent: FC<CardContentProps> = ({ children, className }) => (
  <div className={cn("text-sm", className)}>{children}</div>
);
CardContent.displayName = "CardContent";

type FooterContent = {
  text: string;
  link?: {
    label: string;
    href: string;
  };
};

type CardFooterProps = PropsWithChildren<{
  content?: FooterContent;
  className?: string;
}>;

const CardFooter: FC<CardFooterProps> = ({ content, children, className }) => (
  <div className={cn("border-t px-6 py-4", className)}>
    <div className="flex items-center text-sm text-card-foreground">
      {content ? (
        <>
          {content.text}
          {content.link && (
            <a
              href={content.link.href}
              className="ml-2 text-primary hover:underline"
            >
              {content.link.label}
            </a>
          )}
        </>
      ) : (
        children
      )}
    </div>
  </div>
);
CardFooter.displayName = "CardFooter";

export type CardProps = {
  children: ReactNode | ReactNode[];
  onClick?: () => void;
  href?: string;
  className?: string;
};

const Card: FC<CardProps> = ({ children, onClick, href, className }) => {
  const validChildren = Children.toArray(children);

  const hasButtonElement = Children.toArray(validChildren).some((child) => {
    if (
      React.isValidElement<CardHeaderProps>(child) &&
      child.type === CardHeader
    ) {
      return child.props.rightElement?.type === "button";
    }
    return false;
  });

  if (hasButtonElement && (onClick || href)) {
    console.warn(
      "Card: Card-level interaction (onClick/href) will be ignored when header contains a button element. " +
        "This prevents confusing UX with nested clickable elements.",
    );
  }

  const isInteractive = !hasButtonElement && Boolean(onClick || href);
  const Wrapper = href && !hasButtonElement ? "a" : "div";
  const wrapperProps = !hasButtonElement
    ? href
      ? { href }
      : onClick
        ? { onClick }
        : {}
    : {};

  return (
    <Wrapper
      className={cn(
        "relative flex h-full w-full flex-col border bg-card text-card-foreground",
        isInteractive && "cursor-pointer hover:bg-card/70",
        className,
      )}
      {...wrapperProps}
    >
      <div className="p-6">
        <Stack gap={3}>
          {validChildren.map((child) => {
            if (React.isValidElement(child) && child.type === CardFooter) {
              return null;
            }

            return child;
          })}
        </Stack>
      </div>
      {validChildren.find(
        (child) => React.isValidElement(child) && child.type === CardFooter,
      )}
    </Wrapper>
  );
};

/** Card heading text, styled to the display scale. */
const CardTitle: FC<PropsWithChildren<{ className?: string }>> = ({
  children,
  className,
}) => (
  <div className={cn("text-md leading-none font-semibold", className)}>
    {children}
  </div>
);
CardTitle.displayName = "CardTitle";

/** Secondary line under a {@link CardTitle}. */
const CardDescription: FC<PropsWithChildren<{ className?: string }>> = ({
  children,
  className,
}) => (
  <div
    className={cn("w-full truncate text-sm text-muted-foreground", className)}
  >
    {children}
  </div>
);
CardDescription.displayName = "CardDescription";

/** Right-aligned metadata slot; hidden while an action slot is hovered. */
const CardInfo: FC<PropsWithChildren<{ className?: string }>> = ({
  children,
  className,
}) => (
  <div
    className={cn(
      "ml-auto flex justify-start gap-2",
      "group-hover/card:has([data-slot=card-action]):opacity-0",
      className,
    )}
  >
    {children}
  </div>
);
CardInfo.displayName = "CardInfo";

/** Right-aligned action slot (buttons, menus). */
const CardActions: FC<PropsWithChildren<{ className?: string }>> = ({
  children,
  className,
}) => (
  <div data-slot="card-action" className={cn("flex", className)}>
    {children}
  </div>
);
CardActions.displayName = "CardActions";

type CardEntityProps = {
  children: ReactNode;
  /** Content centered in a bordered box on the icon rail. */
  icon?: ReactNode;
  /** Additional styling for the icon rail surface. */
  iconRailClassName?: string;
  /** Extra content layered on the icon rail (e.g. an "Added" badge). */
  overlay?: ReactNode;
  className?: string;
  onClick?: (e: React.MouseEvent<HTMLDivElement>) => void;
};

/**
 * Horizontal entity card: a flat icon rail on the left, content on the right.
 * Used by catalog, MCP, source and plugin index pages. Replaces the former
 * DotCard — same API, minus the dot-pattern illustration.
 */
const CardEntity: FC<CardEntityProps> = ({
  children,
  icon,
  iconRailClassName,
  className,
  overlay,
  onClick,
}) => (
  <div
    onClick={onClick}
    // Clickable cards are plain divs, so give them button semantics for
    // keyboard and assistive-tech users; non-interactive cards stay plain.
    {...(onClick && {
      role: "button",
      tabIndex: 0,
      onKeyDown: (e: React.KeyboardEvent<HTMLDivElement>) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onClick(e as unknown as React.MouseEvent<HTMLDivElement>);
        }
      },
    })}
    className={cn(
      "group flex h-full min-h-[156px] flex-row overflow-hidden border bg-card text-card-foreground transition-colors",
      "hover:border-neutral-hover",
      className,
    )}
  >
    <div
      className={cn(
        "relative w-40 shrink-0 overflow-hidden border-r bg-surface-secondary-default",
        iconRailClassName,
      )}
    >
      {icon && (
        <div className="absolute inset-0 flex items-center justify-center">
          <div className="border bg-card p-3">{icon}</div>
        </div>
      )}
      {overlay}
    </div>
    <div className="flex min-w-0 flex-1 flex-col p-4">{children}</div>
  </div>
);
CardEntity.displayName = "CardEntity";

type CardDashboardProps = {
  title: string;
  action?: ReactNode;
  children: ReactNode;
  tooltip?: string;
  /** Body classes, e.g. `p-0` for content that should reach the card edges. */
  bodyClassName?: string;
  /** Root classes, e.g. `h-auto` for a panel that should not stretch. */
  className?: string;
};

/**
 * Card.Dashboard — a titled dashboard panel: an eyebrow title bar (with an
 * optional info tooltip and a right-aligned action) over a divider and a body.
 * Formerly the standalone DashboardCard.
 */
function CardDashboard({
  title,
  action,
  children,
  tooltip,
  bodyClassName,
  className,
}: CardDashboardProps): JSX.Element {
  return (
    <div
      className={cn(
        "bg-card text-card-foreground relative flex h-full w-full flex-col border",
        className,
      )}
    >
      <div className="flex w-full flex-row items-center justify-between gap-4 border-b px-6 py-4">
        <div className="flex items-center gap-1.5">
          <h3 className="text-eyebrow">{title}</h3>
          {tooltip && (
            <SimpleTooltip tooltip={tooltip}>
              <button
                type="button"
                aria-label={`About ${title}`}
                className="text-muted-foreground hover:text-foreground inline-flex cursor-help items-center"
              >
                <Icon name="info" className="size-3.5" />
              </button>
            </SimpleTooltip>
          )}
        </div>
        {action}
      </div>
      <div className={cn("px-6 py-5", bodyClassName)}>{children}</div>
    </div>
  );
}

const CardWithSubcomponents = Object.assign(Card, {
  Entity: CardEntity,
  Header: CardHeader,
  Title: CardTitle,
  Description: CardDescription,
  Info: CardInfo,
  Actions: CardActions,
  Content: CardContent,
  Footer: CardFooter,
  Dashboard: CardDashboard,
});

export { CardWithSubcomponents as Card };

/** A card-shaped placeholder for loading states. */
export function CardSkeleton(): React.JSX.Element {
  return (
    <CardWithSubcomponents>
      <CardHeader>
        <CardTitle>
          <Skeleton className="h-4 w-40" />
        </CardTitle>
        <CardDescription>
          <Skeleton className="h-4 w-full" />
        </CardDescription>
      </CardHeader>
      <CardContent>
        <SkeletonParagraph />
      </CardContent>
    </CardWithSubcomponents>
  );
}

export interface CardsProps {
  children?: ReactNode;
  className?: string;
  isLoading?: boolean;
  noGrid?: boolean;
  cardSize?: number;
}

/** Responsive grid of {@link Card}s, with a built-in loading state. */
export function Cards({
  children,
  className,
  isLoading,
  noGrid,
  cardSize = 2,
}: CardsProps): React.JSX.Element {
  const items = isLoading
    ? ["one", "two", "three"].map((key) => (
        <Grid.Item key={key} colSpan={cardSize}>
          <CardSkeleton />
        </Grid.Item>
      ))
    : Children.map(children, (child) => (
        <Grid.Item colSpan={cardSize}>{child}</Grid.Item>
      ));

  if (!items) return <>Nothing found</>;

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
      >
        {items}
      </Grid>
    </div>
  );
}
