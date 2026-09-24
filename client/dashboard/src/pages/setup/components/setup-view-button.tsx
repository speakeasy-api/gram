import { Link, useLocation } from "react-router";
import { ArrowLeft } from "lucide-react";
import { Button } from "@/components/ui/Button";
import { useOrgRoutes } from "@/routes";

export function SetupViewButton({
  wizard = false,
  disabled = false,
  edge,
  className,
}: {
  wizard?: boolean;
  disabled?: boolean;
  edge?: "start" | "end";
  className?: string;
}): JSX.Element {
  const routes = useOrgRoutes();
  const location = useLocation();
  const search = new URLSearchParams(location.search);
  search.delete("view");
  const returningToWorkstreams = wizard && search.get("from") === "workstreams";
  if (wizard) {
    // A task on the board URL reopens its wizard. Return to the board itself,
    // retaining project context and unrelated filters instead.
    search.delete("from");
    search.delete("task");
    search.delete("step");
  }
  if (disabled)
    return (
      <Button
        disabled
        variant="tertiary"
        size="sm"
        edge={edge}
        className={className}
      >
        {returningToWorkstreams && (
          <Button.LeftIcon>
            <ArrowLeft aria-hidden="true" />
          </Button.LeftIcon>
        )}
        <Button.Text>{wizard ? "Workstreams" : "Wizard"}</Button.Text>
      </Button>
    );
  return (
    <Button
      asChild
      variant="tertiary"
      size="sm"
      edge={edge}
      className={className}
    >
      <Link
        to={{
          pathname: wizard ? routes.setup.href() : routes.setupWizard.href(),
          search: search.toString(),
          hash: location.hash,
        }}
      >
        {returningToWorkstreams && (
          <Button.LeftIcon>
            <ArrowLeft aria-hidden="true" />
          </Button.LeftIcon>
        )}
        <Button.Text>{wizard ? "Workstreams" : "Wizard"}</Button.Text>
      </Link>
    </Button>
  );
}
