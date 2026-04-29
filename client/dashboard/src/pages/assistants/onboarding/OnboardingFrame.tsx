import { GramLogo } from "@/components/gram-logo";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { Link } from "react-router";

export function OnboardingFrame({
  onExit,
  children,
}: {
  onExit: () => void;
  children: React.ReactNode;
}) {
  return (
    <div className="bg-background flex h-[100vh] w-full flex-col">
      <Stack
        direction="horizontal"
        align="center"
        justify="space-between"
        className="h-16 w-full shrink-0 border-b px-6"
      >
        <Link className="hover:bg-accent rounded-md p-2" to="/">
          <GramLogo className="w-25" />
        </Link>
        <Button variant="tertiary" size="sm" onClick={onExit}>
          Exit to dashboard
        </Button>
      </Stack>
      <div className="flex-1 overflow-hidden">{children}</div>
    </div>
  );
}
