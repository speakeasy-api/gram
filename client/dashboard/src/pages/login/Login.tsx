import { useSession } from "@/contexts/Auth";
import { useRoutes } from "@/routes";
import { JourneyDemo } from "./components/journey-demo";
import { LoginSection } from "./components/login-section";
import { useSearchParams, useNavigate } from "react-router";
import { useEffect } from "react";

export default function Login() {
  const routes = useRoutes();
  const session = useSession();
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();

  const explicitRedirect = searchParams.get("redirect");
  const disposition = searchParams.get("disposition");
  const redirectTo =
    explicitRedirect ??
    (disposition ? `/?disposition=${encodeURIComponent(disposition)}` : null);
  useEffect(() => {
    if (session.session !== "") {
      if (redirectTo) {
        navigate(redirectTo, { replace: true });
      } else {
        routes.home.goTo();
      }
    }
  }, [session.session, redirectTo, navigate, routes.home]);

  return (
    <main className="flex min-h-screen flex-col md:flex-row">
      <JourneyDemo />
      <LoginSection redirectTo={redirectTo} />
    </main>
  );
}
