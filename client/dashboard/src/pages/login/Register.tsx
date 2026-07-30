import { useSession } from "@/contexts/Auth";
import { useRoutes } from "@/routes";
import { AuthShell } from "./components/auth-shell";
import { RegisterPanel } from "./components/register-panel";
import { Navigate, useSearchParams } from "react-router";

export default function Register(): JSX.Element {
  const routes = useRoutes();
  const session = useSession();
  const [searchParams] = useSearchParams();

  const disposition = searchParams.get("disposition");
  if (disposition === "assistants") {
    return (
      <Navigate
        to={`/login?disposition=${encodeURIComponent(disposition)}`}
        replace
      />
    );
  }

  if (session.activeOrganizationId !== "") {
    const redirect = searchParams.get("redirect");
    if (redirect) {
      window.location.href = redirect;
    } else {
      routes.mcp.goTo();
    }
  }

  return (
    <AuthShell page="Register">
      <RegisterPanel />
    </AuthShell>
  );
}
