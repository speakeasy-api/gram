import { useSession } from "@/contexts/Auth";
import { safeSameOriginUrl } from "@/lib/safe-external-url";
import { useRoutes } from "@/routes";
import { AuthShell } from "./components/auth-shell";
import { RegisterPanel } from "./components/register-panel";
import { SignUpPanel } from "./components/signup-panel";
import { useSearchParams } from "react-router";

export default function SignUp(): JSX.Element {
  const session = useSession();
  const routes = useRoutes();
  const [searchParams] = useSearchParams();
  const redirectTo = safeSameOriginUrl(searchParams.get("redirect"));

  if (session.session !== "" && session.activeOrganizationId !== "") {
    if (redirectTo) {
      window.location.href = redirectTo;
    } else {
      routes.mcp.goTo();
    }
  }

  const panel =
    session.session === "" ? (
      <SignUpPanel />
    ) : (
      <RegisterPanel redirectTo={redirectTo} />
    );

  return (
    <AuthShell page="Sign up" contentClassName="max-w-[400px] gap-5">
      {panel}
    </AuthShell>
  );
}
