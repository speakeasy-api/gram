import { Button } from "@/components/ui/button";
import { useSdkClient } from "@/contexts/Sdk";
import { LogOutIcon } from "lucide-react";

export default function BookDemo() {
  const client = useSdkClient();

  const handleLogout = async () => {
    await client.auth.logout();
    window.location.href = "/login";
  };

  return (
    <div className="bg-background flex min-h-screen items-center justify-center">
      <Button
        variant="ghost"
        size="sm"
        onClick={handleLogout}
        className="absolute top-4 right-4"
      >
        <LogOutIcon className="mr-2 h-4 w-4" />
        Log out
      </Button>
      <div className="mx-auto max-w-md space-y-6 text-center">
        <h1 className="text-3xl font-bold tracking-tight">
          Book a Demo to Access the Speakeasy MCP Platform
        </h1>
        <p className="text-muted-foreground">
          To access the Speakeasy MCP Platform, please book a demo with our
          team.
        </p>
        <Button asChild size="lg">
          <a
            href="https://www.speakeasy.com/book-demo"
            target="_blank"
            rel="noopener noreferrer"
          >
            Book a Demo
          </a>
        </Button>
      </div>
    </div>
  );
}
