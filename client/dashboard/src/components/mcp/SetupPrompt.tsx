import { Button } from "@/components/ui/Button";
import { Link } from "react-router";

/** Overview's first card while a provider's guided setup is unfinished. */
export function SetupPrompt({
  name,
  href,
}: {
  name: string;
  href: string;
}): JSX.Element {
  return (
    <div className="bg-card flex flex-wrap items-center justify-between gap-4 border px-5 py-4">
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="text-sm font-medium">Finish setting up {name}</span>
        <span className="text-muted-foreground text-sm">
          A guided setup connects {name} to Speakeasy, so your team&apos;s
          agents can use it.
        </span>
      </div>
      <Button variant="primary" size="sm" asChild>
        <Link to={href}>
          <Button.Text>Continue setup</Button.Text>
        </Link>
      </Button>
    </div>
  );
}
