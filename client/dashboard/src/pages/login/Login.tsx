import { GramLogo } from "@/components/gram-logo";
import { Page } from "@/components/page-layout";
import { Button } from "@speakeasy-api/moonshine";

export default function Login() {
  const handleLogin = async () => {
    window.location.href = 'http://localhost:8080/rpc/auth.login';
  };

  return (
    <Page>
      <div className="flex flex-col justify-center items-center h-screen gap-8">
        <GramLogo animate className="scale-125" />
        <Button 
          onClick={handleLogin}
          variant="default"
          size="lg"
        >
          Login
        </Button>
      </div>
    </Page>
  );
}
