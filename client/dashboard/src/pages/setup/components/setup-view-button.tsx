import { Link, useLocation } from "react-router";
import { Button } from "@/components/ui/Button";
import { useOrgRoutes } from "@/routes";

export function SetupViewButton({
  wizard = false,
  disabled = false,
}: {
  wizard?: boolean;
  disabled?: boolean;
}): JSX.Element {
  const routes = useOrgRoutes();
  const location = useLocation();
  const search = new URLSearchParams(location.search);
  search.delete("view");
  if (disabled)
    return (
      <Button disabled variant="tertiary" size="sm">
        <Button.Text>{wizard ? "Workstreams" : "Wizard"}</Button.Text>
      </Button>
    );
  return (
    <Button asChild variant="tertiary" size="sm">
      <Link
        to={{
          pathname: wizard ? routes.setup.href() : routes.setupWizard.href(),
          search: search.toString(),
          hash: location.hash,
        }}
      >
        <Button.Text>{wizard ? "Workstreams" : "Wizard"}</Button.Text>
      </Link>
    </Button>
  );
}
