import type { JSX } from "react";
import { Navigate } from "@tanstack/react-router";

// The Gram admin API owns /admin/* on this origin, so the SPA claims the rest.
// Anything the route tree does not match lands back on the organizations list.
export function NotFound(): JSX.Element {
  return <Navigate to="/organizations" replace />;
}
