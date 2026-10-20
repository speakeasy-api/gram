import { useRoutes } from "@/routes";
import { Navigate } from "react-router";

/** Keeps existing settings links pointed at the gateway's MCP home. */
export default function OrgGateway(): JSX.Element {
  const routes = useRoutes();
  return <Navigate to={routes.mcp.organizationGateway.href()} replace />;
}
