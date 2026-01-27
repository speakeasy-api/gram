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

  const redirectTo = searchParams.get("redirect");
  const skipRedirect = searchParams.get("preview") === "true";
  useEffect(() => {
    if (session.session !== "" && !skipRedirect) {
      if (redirectTo) {
        navigate(redirectTo, { replace: true });
      } else {
        routes.home.goTo();
      }
    }
  }, [session.session, searchParams, navigate, routes.home, skipRedirect]);

  return (
    <main className="flex min-h-screen flex-col md:flex-row">
      <JourneyDemo />
      <LoginSection redirectTo={redirectTo} />
    </main>
  );
}
