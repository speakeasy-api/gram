import type { ReactNode } from "react";
import { useLocation } from "react-router";
import { ContentErrorBoundary } from "./content-error-boundary";

type AppRouteContentErrorBoundaryProps = {
  children: ReactNode;
  fallback?: ReactNode;
};

export function AppRouteContentErrorBoundary({
  children,
  fallback,
}: AppRouteContentErrorBoundaryProps): JSX.Element {
  const { key } = useLocation();

  return (
    <ContentErrorBoundary resetKeys={[key]} fallback={fallback}>
      {children}
    </ContentErrorBoundary>
  );
}
