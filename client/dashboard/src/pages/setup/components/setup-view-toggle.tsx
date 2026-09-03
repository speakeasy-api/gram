import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { useOrgRoutes } from "@/routes";

type SetupView = "wizard" | "board";

export function SetupViewToggle({ view }: { view: SetupView }): JSX.Element {
  const routes = useOrgRoutes();

  return (
    <div role="group" aria-label="Setup view">
      <SegmentedControl
        value={view}
        onChange={(nextView) => {
          if (nextView === view) return;
          if (nextView === "board") routes.setupBoard.goTo();
          else routes.setup.goTo();
        }}
        options={[
          { value: "wizard", label: "Wizard" },
          { value: "board", label: "Board" },
        ]}
      />
    </div>
  );
}
