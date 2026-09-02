import { ThemeSwitcher } from "@/components/ui/ThemeSwitcher";

export function OnboardingFooter(): JSX.Element {
  return (
    <footer className="border-border bg-background w-full border-t">
      <div className="mx-auto flex w-full max-w-7xl items-center justify-between px-8 py-4">
        <ThemeSwitcher />
        <span className="text-muted-foreground text-sm">Speakeasy 2026</span>
      </div>
    </footer>
  );
}
