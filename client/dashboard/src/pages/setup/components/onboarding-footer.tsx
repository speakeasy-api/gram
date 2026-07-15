import { ThemeSwitcher } from "@/components/ui/theme-switcher";
import { Type } from "@/components/ui/type";

export function OnboardingFooter(): JSX.Element {
  return (
    <footer className="border-border bg-background w-full border-t">
      <div className="mx-auto flex w-full max-w-5xl items-center justify-between py-4">
        <ThemeSwitcher />
        <Type small muted>
          Speakeasy 2026
        </Type>
      </div>
    </footer>
  );
}
