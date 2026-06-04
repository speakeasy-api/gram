import { ThemeToggle } from "@/components/ui/theme-toggle";

export function OnboardingFooter() {
  return (
    <footer className="border-border bg-background w-full border-t">
      <div className="mx-auto flex w-full max-w-5xl items-center justify-between py-4">
        <ThemeToggle />
        <span className="text-muted-foreground text-sm">Speakeasy 2026</span>
      </div>
    </footer>
  );
}
