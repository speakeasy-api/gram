import { PageEyebrow } from "@/components/page-eyebrow";
import { Button } from "@/components/ui/Button";
import { useRoutes } from "@/routes";

/**
 * Catch-all for unmatched paths inside the app shell (e.g. a mistyped page
 * slug like /projects/default/budgets). Without this, React Router renders
 * nothing and the content sheet is silently blank.
 */
export default function NotFound(): JSX.Element {
  const routes = useRoutes();
  return (
    <div className="flex h-full min-h-[60vh] w-full flex-col items-center justify-center gap-4 p-8">
      <PageEyebrow area="404" />
      <h1 className="text-display-sm font-thin">Page not found</h1>
      <p className="text-muted-foreground max-w-md text-center text-sm">
        This page doesn't exist — the link may be outdated.
      </p>
      <Button variant="secondary" href={routes.home.href()}>
        Back to Home
      </Button>
    </div>
  );
}
