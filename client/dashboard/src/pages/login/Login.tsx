import { Page } from "@/components/page-layout"
import { LoginSection } from "./components/login-section"
import { PromptsSection } from "./components/prompt-section"
import { useSession } from "@/contexts/Auth";
import { Navigate } from "react-router-dom";

export default function Login() {
  const session = useSession();
  if (session.session !== "") { // we are logged in, redirect to the home page
    return <Navigate to="/" />
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
  )
}