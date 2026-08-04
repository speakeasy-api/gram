import { createFileRoute, Link, Outlet } from "@tanstack/react-router";
import { cn } from "@/lib/utils";

export const Route = createFileRoute("/ema")({
  component: EmaLayout,
});

/**
 * Sub-nav ordered by the shape of the flow rather than alphabetically: the
 * two things that exist (apps, resources), then the two policies over them
 * (who is assigned, who is trusted), then what to do with it and what came
 * out.
 */
const GROUPS: ReadonlyArray<{
  title: string;
  items: ReadonlyArray<{ to: string; label: string }>;
}> = [
  {
    title: "Registry",
    items: [
      { to: "/ema/apps", label: "Apps" },
      { to: "/ema/resources", label: "Resources" },
    ],
  },
  {
    title: "Policy",
    items: [
      { to: "/ema/assignments", label: "Assignments" },
      { to: "/ema/trust-rules", label: "Trust rules" },
    ],
  },
  {
    title: "Exercise",
    items: [
      { to: "/ema/playground", label: "Playground" },
      { to: "/ema/issued", label: "Issued grants" },
    ],
  },
];

function EmaLayout() {
  return (
    <div className="max-w-5xl mx-auto grid grid-cols-[200px_1fr] gap-8">
      <nav className="flex flex-col gap-5">
        {GROUPS.map((group) => (
          <section key={group.title} className="flex flex-col gap-1">
            <h3 className="text-[10px] uppercase tracking-wider text-muted-foreground/80 font-mono px-3 mb-1">
              {group.title}
            </h3>
            {group.items.map((item) => (
              <Link
                key={item.to}
                to={item.to}
                className={cn(
                  "px-3 py-2 rounded-md text-sm transition-colors",
                  "text-muted-foreground hover:text-foreground hover:bg-accent/50",
                )}
                activeProps={{
                  className:
                    "bg-accent text-foreground hover:bg-accent hover:text-foreground",
                }}
              >
                <span className="font-medium">{item.label}</span>
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
