import { useSession } from "@/contexts/Auth";
import { useRoutes } from "@/routes";
import { JourneyDemo } from "./components/journey-demo";
import { RegisterSection } from "./components/login-section";
import { useSearchParams } from "react-router";

export default function Register() {
  const routes = useRoutes();
  const session = useSession();
  const [searchParams] = useSearchParams();

  if (session.activeOrganizationId !== "") {
    const destination =
      searchParams.get("returnTo") ?? searchParams.get("redirect");
    if (destination) {
      window.location.href = destination;
    } else {
      routes.mcp.goTo();
    }
  }

  return (
    <main className="flex min-h-screen flex-col md:flex-row">
      <JourneyDemo />
      <RegisterSection />
    </main>
  );
}
