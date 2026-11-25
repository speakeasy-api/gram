import { Button } from "@/components/ui/Button";
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/HoverCard";
import { useRoutes } from "@/routes";
import { HoverCardPortal } from "@radix-ui/react-hover-card";
import { CircleAlert } from "lucide-react";

/**
 * The per-source failure mark: a destructive alert that explains itself on
 * hover and links to the deployment whose logs name the error.
 */
export function SourceFailureNotice({
  deploymentId,
  className,
}: {
  deploymentId: string | undefined;
  className?: string;
}): JSX.Element {
  const routes = useRoutes();

  return (
    <HoverCard>
      <HoverCardTrigger
        className={className}
        aria-label="View deployment failure details"
      >
        <CircleAlert className="text-destructive size-3.5" />
      </HoverCardTrigger>
      <HoverCardPortal>
        <HoverCardContent side="bottom" className="text-sm" asChild>
          <div>
            <div>
              This source caused the latest deployment to fail. Upload a new
              version or remove it to prevent future failures.
            </div>
            {deploymentId && (
              <div className="mt-3 flex justify-end">
                <routes.deployments.deployment.Link
                  className="text-link"
                  params={[deploymentId]}
                >
                  View logs
                </routes.deployments.deployment.Link>
              </div>
            )}
          </div>
        </HoverCardContent>
      </HoverCardPortal>
    </HoverCard>
  );
}

/**
 * The toolbar's warning that the latest push failed, linking to its logs.
 * Renders nothing while the project's deployments are healthy.
 */
export function DeploymentErrorsButton({
  failedDeploymentId,
}: {
  failedDeploymentId: string | undefined;
}): JSX.Element | null {
  const routes = useRoutes();
  if (!failedDeploymentId) return null;

  return (
    // h-10 matches the toolbar's own controls, as the button beside it does.
    <Button variant="secondary" className="text-destructive h-10" asChild>
      <routes.deployments.deployment.Link params={[failedDeploymentId]}>
        <Button.LeftIcon>
          <CircleAlert className="size-4" />
        </Button.LeftIcon>
        <Button.Text>Deployment errors</Button.Text>
      </routes.deployments.deployment.Link>
    </Button>
  );
}
