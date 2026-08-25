import { useSession } from "@/contexts/Auth";
import { safeSameOriginPath, safeSameOriginUrl } from "@/lib/safe-external-url";
import { AuthShell } from "./components/auth-shell";
import { RegisterPanel } from "./components/register-panel";
import { SignUpPanel } from "./components/signup-panel";
import { Navigate, useSearchParams } from "react-router";

export default function SignUp(): JSX.Element {
  const session = useSession();
  const [searchParams] = useSearchParams();
  const redirect = searchParams.get("redirect");
  const redirectTo = safeSameOriginUrl(redirect);

  if (session.session !== "" && session.activeOrganizationId !== "") {
    return <Navigate to={safeSameOriginPath(redirect) ?? "/"} replace />;
  }

  const panel =
    session.session === "" ? (
      <SignUpPanel redirectTo={redirectTo} />
    ) : (
      <RegisterPanel redirectTo={redirectTo} />
    );

  return (
    <AuthShell page="Sign up" contentClassName="max-w-[400px] gap-5">
      {panel}
    </AuthShell>
  );
}
