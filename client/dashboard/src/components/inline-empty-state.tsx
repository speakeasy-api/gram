import { Heading } from "@/components/ui/Heading";
import { Icon } from "@/components/ui/Icon";
import { type IconName } from "@/components/ui/Icon/names";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import type { ReactNode } from "react";

/**
 * InlineEmptyState — the shared "nothing here yet" panel for empty regions
 * inside a page (a table area, a card grid, a section) — as opposed to the
 * full-page {@link EmptyState} in `page-layout`.
 *
 * It renders the editorial idiom the design system calls for: a square dashed
 * hairline frame and a square hairline icon tile — NOT a gray circle blob or a
 * tinted wash. Roughly a dozen pages hand-rolled this block with
 * `bg-muted/20 … border-dashed` + a `rounded-full` icon circle; route those
 * through here instead so the look changes in one place.
 *
 *   <InlineEmptyState
 *     icon="inbox"
 *     heading="No API keys yet"
 *     description="Create a key to start calling the API."
 *     action={<Button size="sm">New key</Button>}
 *   />
 */
export function InlineEmptyState({
  icon,
  graphic,
  heading,
  description,
  action,
  orientation = "vertical",
  className,
}: {
  /** Icon name for the square hairline tile. Ignored when `graphic` is set. */
  icon?: IconName;
  /** Custom graphic in place of the icon tile. */
  graphic?: ReactNode;
  heading: string;
  description?: string;
  /** CTA node (button/link). Rendered below the copy. */
  action?: ReactNode;
  /** "vertical" (default) centers the stack; "horizontal" lays it in a row. */
  orientation?: "vertical" | "horizontal";
  className?: string;
}): JSX.Element {
  const tile =
    graphic ??
    (icon ? (
      <div className="text-muted-foreground flex h-12 w-12 shrink-0 items-center justify-center border">
        <Icon name={icon} className="h-5 w-5" aria-hidden="true" />
      </div>
    ) : null);

  if (orientation === "horizontal") {
    return (
      <div
        className={cn(
          "flex w-full items-center gap-4 border border-dashed px-6 py-8",
          className,
        )}
      >
        {tile}
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <Heading variant="h5" className="font-medium">
            {heading}
          </Heading>
          {description != null && (
            <Text small muted>
              {description}
            </Text>
          )}
        </div>
        {action != null && <div className="shrink-0">{action}</div>}
      </div>
    );
  }

  return (
    <div
      className={cn(
        "flex w-full flex-col items-center justify-center border border-dashed px-8 py-16 text-center",
        className,
      )}
    >
      {tile != null && <div className="mb-4">{tile}</div>}
      <Heading variant="h5" className="font-medium">
        {heading}
      </Heading>
      {description != null && (
        <Text small muted className="mt-1 max-w-md">
          {description}
        </Text>
      )}
      {action != null && <div className="mt-4">{action}</div>}
    </div>
  );
}
