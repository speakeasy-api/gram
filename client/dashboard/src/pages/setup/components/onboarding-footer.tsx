import { ThemeSwitcher } from "@/components/ui/ThemeSwitcher";
import { cn } from "@/lib/utils";
import { SETUP_CONTAINER } from "./setup-container";

export function OnboardingFooter(): JSX.Element {
  return (
    <footer className="border-border bg-background w-full border-t">
      <div
        className={cn(
          SETUP_CONTAINER,
          "flex items-center justify-between py-4",
        )}
      >
        <ThemeSwitcher />
        <span className="text-muted-foreground text-sm">Speakeasy 2026</span>
      </div>
    </footer>
  );
}
