import { Button } from "@/components/ui/button";

export default function BookDemo() {
  return (
    <div className="flex min-h-screen items-center justify-center bg-background">
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
