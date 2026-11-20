import type { ReactNode } from "react";
import { Navigate } from "react-router";
import { useAssistantsAvailability } from "@/hooks/useAssistantsAvailability";
import { useRoutes } from "@/routes";

/**
 * Renders assistant pages only on a deployment that runs assistants. A direct
 * link on a deployment without them goes to the project home, the same place
 * the hidden navigation would have left the user. Nothing renders while the
 * deployment's setting loads, so the pages never flash before redirecting.
 */
export function RequireAssistantsAvailable({
  children,
}: {
  children: ReactNode;
}): JSX.Element | null {
  const routes = useRoutes();
  const availability = useAssistantsAvailability();
  if (availability === "loading") return null;
  if (availability === "unavailable") {
    return <Navigate to={routes.home.href()} replace />;
  }
  return <>{children}</>;
}
