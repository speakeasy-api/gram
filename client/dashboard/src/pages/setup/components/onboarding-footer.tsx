export function OnboardingFooter() {
  return (
    <footer className="border-border bg-card flex items-center justify-between border-t px-8 py-4">
      <div className="flex items-center gap-6">
        <a
          href="#"
          className="text-muted-foreground hover:text-foreground text-sm transition-colors"
        >
          Help Center
        </a>
        <a
          href="#"
          className="text-muted-foreground hover:text-foreground text-sm transition-colors"
        >
          Status
        </a>
        <a
          href="#"
          className="text-muted-foreground hover:text-foreground text-sm transition-colors"
        >
          Docs
        </a>
        <a
          href="#"
          className="text-muted-foreground hover:text-foreground text-sm transition-colors"
        >
          Contact Support
        </a>
      </div>
      <span className="text-muted-foreground text-sm">Speakeasy 2026</span>
    </footer>
  );
}
