import { Page } from "@/components/page-layout";
import { LoginSection } from "./components/login-section";
import { PromptsSection } from "./components/prompt-section";
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
    <Page>
      <main
        className="flex min-h-screen flex-col md:flex-row"
        style={{
          /* Apply the main font family from CSS variables */
          fontFamily: "var(--font-dm-sans)",
        }}
      >
        <LoginSection />
        <PromptsSection />
      </main>
    </Page>
  );
}
