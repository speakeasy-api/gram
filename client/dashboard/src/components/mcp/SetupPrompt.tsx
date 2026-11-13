import { StatusBanner } from "@/components/status-banner";
import { Button } from "@/components/ui/Button";
import { BookOpen, TriangleAlert } from "lucide-react";
import { Link } from "react-router";

/**
 * Overview's banner while a provider's setup is unfinished, in the plugin
 * banner's shape, so a step easy to miss in the readiness list is the first
 * thing on the page. It opens the Setup modal.
 */
export function SetupPrompt({
  name,
  href,
  description,
  action = "Continue setup",
}: {
  name: string;
  href: string;
  /** What the setup involves; defaults to the guided flow's words. */
  description?: string;
  action?: string;
}): JSX.Element {
  return (
    <StatusBanner tone="warning">
      <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-4 px-5 py-4">
        <div className="flex min-w-0 flex-col gap-1">
          <span className="text-warning-foreground flex items-center gap-2 text-sm font-medium">
            <TriangleAlert className="size-4 shrink-0" aria-hidden />
            Finish setting up {name}
          </span>
          <span className="text-muted-foreground pl-6 text-sm">
            {description ??
              `A guided setup connects ${name} to Speakeasy, so your team's agents can use it.`}
          </span>
        </div>
        <Button variant="primary" size="sm" asChild>
          <Link to={href}>
            <Button.LeftIcon>
              <BookOpen />
            </Button.LeftIcon>
            <Button.Text>{action}</Button.Text>
          </Link>
        </Button>
      </div>
    </StatusBanner>
  );
}
