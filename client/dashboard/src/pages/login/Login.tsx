import { useSession } from "@/contexts/Auth";
import { useRoutes } from "@/routes";
import { buildLoginRedirectURL, isSetupDomain } from "@/lib/utils";
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
  // On setup domain, redirect back to the setup origin after auth so the
  // server doesn't send us to the default GRAM_SITE_URL (app domain).
  const redirectTo = isSetupDomain()
    ? window.location.origin
    : (explicitRedirect ??
      (disposition
        ? `/?disposition=${encodeURIComponent(disposition)}`
        : null));
  useEffect(() => {
    if (session.session !== "") {
      // Disposition signups must reach the server-side Callback so
      // auto-provision can run for users with zero orgs. Force an IDP
      // round-trip even when a session already exists.
      if (disposition && redirectTo) {
        window.location.href = buildLoginRedirectURL(redirectTo);
        return;
      }
      // On setup domain, always navigate to the setup wizard after auth
      if (isSetupDomain()) {
        navigate("/", { replace: true });
        return;
      }
      if (redirectTo) {
        navigate(redirectTo, { replace: true });
      } else {
        routes.home.goTo();
      }
    }
  }, [session.session, disposition, redirectTo, navigate, routes.home]);

  return (
    <main className="flex min-h-screen flex-col md:flex-row">
      <JourneyDemo />
      <LoginSection redirectTo={redirectTo} />
    </main>
  );
}
