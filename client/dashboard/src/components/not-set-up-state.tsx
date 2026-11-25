import { Button } from "@/components/ui/Button";
import { Heading } from "@/components/ui/Heading";
import { Icon } from "@/components/ui/Icon";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import type { ReactNode } from "react";

type NotSetUpStateProps = {
  heading: string;
  description: string;
  action?: ReactNode;
  screenshot?: ReactNode;
  setupHref?: string;
  setupLabel?: string;
  demoHref?: string;
  className?: string;
};

/** Full-feature empty state for a page whose prerequisite is not configured. */
export function NotSetUpState({
  heading,
  description,
  action,
  screenshot,
  setupHref,
  setupLabel = "View setup guide",
  demoHref,
  className,
}: NotSetUpStateProps): JSX.Element {
  return (
    <section
      className={cn(
        "border-border bg-background flex w-full flex-col items-center border px-6 py-12",
        className,
      )}
    >
      <div className="flex max-w-2xl flex-col items-center text-center">
        <Heading variant="h3" className="font-medium">
          {heading}
        </Heading>
        <Text muted className="mt-2 max-w-xl">
          {description}
        </Text>

        <div className="mt-6 flex flex-wrap items-center justify-center gap-2">
          {action}
          {setupHref && (
            <Button variant="secondary" asChild>
              <a href={setupHref}>
                <Button.Text>{setupLabel}</Button.Text>
                <Button.RightIcon>
                  <Icon name="arrow-right" />
                </Button.RightIcon>
              </a>
            </Button>
          )}
          {demoHref && (
            <Button variant="tertiary" asChild>
              <a href={demoHref} target="_blank" rel="noopener noreferrer">
                <Button.Text>View in demo</Button.Text>
                <Button.RightIcon>
                  <Icon name="external-link" />
                </Button.RightIcon>
              </a>
            </Button>
          )}
        </div>
      </div>

      <div className="border-border mt-10 flex w-full max-w-[1400px] items-center justify-center overflow-hidden border bg-white shadow-sm">
        {screenshot ?? (
          <div
            aria-hidden="true"
            className="text-muted-foreground flex flex-col items-center gap-2"
          >
            <Icon name="image" className="size-6" aria-hidden="true" />
            <Text small muted>
              Feature screenshot
            </Text>
          </div>
        )}
      </div>
    </section>
  );
}
