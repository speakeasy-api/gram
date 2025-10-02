import { useSession } from "@/contexts/Auth";
import { useRoutes } from "@/routes";
import { JourneyDemo } from "./components/journey-demo";
import { RegisterSection } from "./components/login-section";

export default function Register() {
  const routes = useRoutes();
  const session = useSession();

  if (session.activeOrganizationId !== "") {
    routes.toolsets.goTo();
  }

  return (
    <main className="flex min-h-screen flex-col md:flex-row">
      <JourneyDemo />
      <RegisterSection />
    </main>
  );
}
