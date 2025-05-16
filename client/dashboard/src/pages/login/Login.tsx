import { LoginSection } from "./components/login-section";
import { JourneyDemo } from "./components/journey-demo";
import { useSession } from "@/contexts/Auth";
import { useRoutes } from "@/routes";

export default function Login() {
  const routes = useRoutes();
  const session = useSession();

  console.log(session);

  if (session.session !== "") {
    // we are logged in, redirect to the home page
    routes.home.goTo();
  }

  return (
    <main className="flex min-h-screen flex-col md:flex-row">
      <JourneyDemo />
      <LoginSection />
    </main>
  );
}
