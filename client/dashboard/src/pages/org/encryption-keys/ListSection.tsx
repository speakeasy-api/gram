import { Text } from "@/components/ui/Text";
import type { ReactNode } from "react";

// ListSection is one titled block on a page that stacks more than one resource
// list: an eyebrow heading, an optional explanation, an optional action on the
// right, and the list itself. The page keeps its single title; these are the
// secondary headings beneath it, so the eyebrow is a real heading for assistive
// technology while keeping the overline styling.
export function ListSection({
  eyebrow,
  description,
  action,
  children,
}: {
  eyebrow: string;
  description?: string;
  action?: ReactNode;
  children: ReactNode;
}): JSX.Element {
  return (
    <section className="flex flex-col gap-4">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex flex-col gap-1">
          <h2 className="text-eyebrow">{eyebrow}</h2>
          {description && (
            <Text small muted className="max-w-3xl">
              {description}
            </Text>
          )}
        </div>
        {action}
      </div>
      {children}
    </section>
  );
}
