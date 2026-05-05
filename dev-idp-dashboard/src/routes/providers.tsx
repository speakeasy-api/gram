import { createFileRoute, Link, Outlet } from "@tanstack/react-router";
import { cn } from "@/lib/utils";
import { MODE_GROUPS, MODE_LABELS } from "@/lib/mode-labels";

export const Route = createFileRoute("/providers")({
  component: ProvidersLayout,
});

function ProvidersLayout() {
  return (
    <div className="max-w-5xl mx-auto grid grid-cols-[200px_1fr] gap-8">
      <nav className="flex flex-col gap-5">
        {MODE_GROUPS.map((group) => (
          <section key={group.title} className="flex flex-col gap-1">
            <h3 className="text-[10px] uppercase tracking-wider text-muted-foreground/80 font-mono px-3 mb-1">
              {group.title}
            </h3>
            {group.modes.map((m) => (
              <Link
                key={m}
                to="/providers/$mode"
                params={{ mode: m }}
                className={cn(
                  "px-3 py-2 rounded-md text-sm transition-colors",
                  "text-muted-foreground hover:text-foreground hover:bg-accent/50",
                )}
                activeProps={{
                  className:
                    "bg-accent text-foreground hover:bg-accent hover:text-foreground",
                }}
              >
                <span className="font-medium">{MODE_LABELS[m]}</span>
              </Link>
            ))}
          </section>
        ))}
      </nav>
      <div className="min-w-0">
        <Outlet />
      </div>
    </div>
  );
}
