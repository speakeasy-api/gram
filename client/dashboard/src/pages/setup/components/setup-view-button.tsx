import { LayoutGrid, ListChecks } from "lucide-react";
import { useParams } from "react-router";
import { Button } from "@/components/ui/Button";
import { useOrgRoutes } from "@/routes";

export type SetupView = "board" | "wizard";

// A single discreet control in the setup header that swaps between the two
// ways through setup: the board (assign and track cards) and the wizard (walk
// them in order). It always names the view you are not on. From a card's own
// page it carries that card into the wizard so the reader lands where they
// were.
export function SetupViewButton({ view }: { view: SetupView }): JSX.Element {
  const routes = useOrgRoutes();
  const { taskSlug } = useParams<{ taskSlug?: string }>();
  const className =
    "text-muted-foreground hover:text-foreground inline-flex gap-1.5 hover:no-underline";

  if (view === "wizard") {
    return (
      <Button asChild variant="tertiary" size="sm" className={className}>
        <routes.setup.Link>
          <LayoutGrid className="h-4 w-4" />
          Board
        </routes.setup.Link>
      </Button>
    );
  }

  return (
    <Button asChild variant="tertiary" size="sm" className={className}>
      <routes.setupWizard.Link queryParams={taskSlug ? { task: taskSlug } : {}}>
        <ListChecks className="h-4 w-4" />
        Wizard
      </routes.setupWizard.Link>
    </Button>
  );
}
