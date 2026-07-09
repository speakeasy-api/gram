"use client";

import React, {
  useLayoutEffect,
  useRef,
  useState,
  isValidElement,
  cloneElement,
  Children,
} from "react";
import type { ReactNode } from "react";
import { cn } from "@/components/ui/moonshine/lib/utils";
import { Text } from "../Text";
import { Heading } from "../Heading";

const TIMELINE_STYLES = {
  line: "bg-surface-tertiary-default",
  iconContainer:
    "relative z-10 flex h-12 w-12 flex-shrink-0 items-center justify-center rounded-full",
  iconBackground:
    "bg-surface-primary-default border-border-neutral-softest absolute inset-0 rounded-full border-2 transition-all duration-300",
  iconContent: "relative z-10 flex h-full w-full items-center justify-center",
  iconIcon:
    "text-muted flex items-center justify-center transition-colors duration-300 [&>svg]:h-4 [&>svg]:w-4",
  iconNumber: "text-muted text-sm font-medium transition-colors duration-300",
  transition: "transition-opacity duration-300",
} as const;

/** @public — kept for upcoming design-system adoption (Kbd/stories) */
export interface TimelineItemProps {
  icon?: ReactNode;
  children: ReactNode;
  className?: string;
  index?: number;
  isLast?: boolean;
  setsize?: number;
}

function TimelineItem({
  icon,
  children,
  className,
  index = 0,
  isLast = false,
  setsize,
}: TimelineItemProps) {
  const enhancedChildren = Children.map(children, (child) => {
    if (
      isValidElement<TimelineContentProps>(child) &&
      child.type === TimelineContent
    ) {
      return cloneElement(child, {
        isLast,
      });
    }
    return child;
  });

  return (
    <li
      className={cn("relative flex items-start gap-4", className)}
      data-timeline-item
      aria-posinset={index + 1}
      aria-setsize={setsize}
    >
      <div className={TIMELINE_STYLES.iconContainer}>
        <div className={TIMELINE_STYLES.iconBackground} />
        <div className={TIMELINE_STYLES.iconContent}>
          {icon ? (
            <div className={TIMELINE_STYLES.iconIcon}>{icon}</div>
          ) : (
            <div className={TIMELINE_STYLES.iconNumber}>{index + 1}</div>
          )}
        </div>
      </div>

      {enhancedChildren}
    </li>
  );
}

/** @public — kept for upcoming design-system adoption (Kbd/stories) */
export interface TimelineContentProps {
  children: ReactNode;
  className?: string;
  isLast?: boolean;
}

function TimelineContent({
  children,
  className,
  isLast,
}: TimelineContentProps) {
  const childrenArray = Children.toArray(children);
  let titleElement: React.ReactNode = null;
  let timestampElement: React.ReactNode = null;
  const otherChildren: React.ReactNode[] = [];

  childrenArray.forEach((child) => {
    if (isValidElement(child)) {
      if (child.type === TimelineTitle) {
        titleElement = child;
      } else if (child.type === TimelineTimestamp) {
        timestampElement = child;
      } else {
        otherChildren.push(child);
      }
    } else {
      otherChildren.push(child);
    }
  });

  return (
    <div
      className={cn("min-w-0 flex-1", !isLast && "pb-6", className)}
      data-testid={isLast ? "timeline-content-last" : "timeline-content"}
    >
      {(titleElement || timestampElement) && (
        <div className="mb-1 flex items-start justify-between gap-4">
          <div className="min-w-0 flex-1">{titleElement}</div>
          {timestampElement && (
            <div className="flex-shrink-0">{timestampElement}</div>
          )}
        </div>
      )}
      {otherChildren.length > 0 && (
        <div className="space-y-1">{otherChildren}</div>
      )}
    </div>
  );
}

/** @public — kept for upcoming design-system adoption (Kbd/stories) */
export interface TimelineTitleProps {
  children: ReactNode;
  className?: string;
  as?: "h1" | "h2" | "h3" | "h4" | "h5" | "h6" | "div";
}

function TimelineTitle({
  children,
  className,
  as = "div",
}: TimelineTitleProps) {
  if (as === "div") {
    return (
      <div
        className={cn("text-heading-xs", TIMELINE_STYLES.transition, className)}
      >
        {children}
      </div>
    );
  }

  return (
    <Heading
      variant="xs"
      as={as}
      className={cn(TIMELINE_STYLES.transition, className)}
    >
      {children}
    </Heading>
  );
}

/** @public — kept for upcoming design-system adoption (Kbd/stories) */
export interface TimelineDescriptionProps {
  children: ReactNode;
  className?: string;
}

function TimelineDescription({
  children,
  className,
}: TimelineDescriptionProps) {
  return (
    <Text
      variant="sm"
      muted
      className={cn("leading-relaxed", TIMELINE_STYLES.transition, className)}
    >
      {children}
    </Text>
  );
}

/** @public — kept for upcoming design-system adoption (Kbd/stories) */
export interface TimelineTimestampProps {
  children: ReactNode;
  className?: string;
}

function TimelineTimestamp({ children, className }: TimelineTimestampProps) {
  return (
    <Text
      variant="xs"
      muted
      className={cn(TIMELINE_STYLES.transition, className)}
    >
      {children}
    </Text>
  );
}

/** @public — kept for upcoming design-system adoption (Kbd/stories) */
export interface TimelineRootProps {
  children: ReactNode;
  className?: string;
  hasMore?: boolean;
}

function TimelineRoot({
  children,
  className,
  hasMore = false,
}: TimelineRootProps) {
  const childrenArray = Children.toArray(children);
  const itemsOnly = childrenArray.filter(
    (child) => isValidElement(child) && child.type === TimelineItem,
  );
  const itemCount = itemsOnly.length;
  const timelineRef = useRef<HTMLDivElement>(null);
  const [lineHeight, setLineHeight] = useState<number | null>(null);

  let itemIndex = 0;
  const enhancedChildren = childrenArray.map((child) => {
    if (
      isValidElement<TimelineItemProps>(child) &&
      child.type === TimelineItem
    ) {
      const cloned = cloneElement(child, {
        index: itemIndex,
        isLast: itemIndex === itemCount - 1,
        setsize: itemCount,
      });
      itemIndex += 1;
      return cloned;
    }
    return child;
  });

  useLayoutEffect(() => {
    if (timelineRef.current && itemCount > 1) {
      const timelineItems = timelineRef.current.querySelectorAll(
        "[data-timeline-item]",
      );
      if (timelineItems.length >= 2) {
        const lastItem = timelineItems[timelineItems.length - 1] as HTMLElement;

        // Calculate distance from center of first icon to center of last icon
        const firstIconCenter = 24; // 1.5rem (top-6) + 0.75rem (half of 3rem icon height)
        const lastItemTop = lastItem.offsetTop;
        const baseHeight = lastItemTop + 24 - firstIconCenter; // 24px = center of last icon

        // If hasMore is true, extend the line by an additional 2rem (32px) past the last icon
        const calculatedHeight = hasMore ? baseHeight + 32 : baseHeight;

        setLineHeight(calculatedHeight);
      }
    }
  }, [itemCount, children, hasMore]);

  return (
    <div className={cn("relative", className)}>
      {itemCount > 0 && (
        <div className="relative" ref={timelineRef}>
          {itemCount > 1 && (
            <div
              className={cn(TIMELINE_STYLES.line, "absolute top-6 left-6 w-px")}
              style={{
                height: lineHeight
                  ? `${lineHeight}px`
                  : hasMore
                    ? `calc(100% - 2.5rem)`
                    : `calc(100% - 4.5rem)`,
              }}
              aria-hidden="true"
              data-testid="timeline-connector"
            />
          )}
          <ol className="space-y-6">{enhancedChildren}</ol>
        </div>
      )}
    </div>
  );
}

const Timeline = Object.assign(TimelineRoot, {
  Item: TimelineItem,
  Content: TimelineContent,
  Title: TimelineTitle,
  Description: TimelineDescription,
  Timestamp: TimelineTimestamp,
});

/** @public — kept for upcoming design-system adoption (Kbd/stories) */
export { Timeline };
