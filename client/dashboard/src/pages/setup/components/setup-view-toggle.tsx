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
          if (nextView === "wizard") routes.setupWizard.goTo();
          else routes.setup.goTo();
        }}
        options={[
          { value: "board", label: "Board" },
          { value: "wizard", label: "Wizard" },
        ]}
      />
    </div>
  );
}
