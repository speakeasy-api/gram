import { useSession } from "@/contexts/Auth";
import { safeSameOriginUrl } from "@/lib/safe-external-url";
import { Navigate, useSearchParams } from "react-router";

export default function Register(): JSX.Element {
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

  const redirect = safeSameOriginUrl(searchParams.get("redirect"));
  const signUpTarget = redirect
    ? "/sign-up?redirect=" + encodeURIComponent(redirect)
    : "/sign-up";

  if (session.session === "" || session.activeOrganizationId === "") {
    return <Navigate to={signUpTarget} replace />;
  }

  return <Navigate to={redirect ?? "/"} replace />;
}
