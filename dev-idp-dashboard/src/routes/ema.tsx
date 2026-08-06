import { createFileRoute, Link, Outlet } from "@tanstack/react-router";
import { cn } from "@/lib/utils";

export const Route = createFileRoute("/ema")({
  component: EmaLayout,
});

/**
 * Policy is the canvas — apps, users, resources and the assignments between
 * them — so it is the landing page rather than an entry in this list. What
 * remains are the two views that are not entity graphs: a way to exercise the
 * flow, and a record of what came out.
 */
const ITEMS: ReadonlyArray<{ to: string; label: string }> = [
  { to: "/ema", label: "Policy" },
  { to: "/ema/playground", label: "Playground" },
  { to: "/ema/issued", label: "Issued grants" },
];

function EmaLayout() {
  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <nav className="inline-flex w-fit items-center gap-1 rounded-full bg-muted p-1">
        {ITEMS.map((item) => (
          <Link
            key={item.to}
            to={item.to}
            activeOptions={{ exact: item.to === "/ema" }}
            className={cn(
              "rounded-full px-3 py-1 text-sm transition-colors",
              "text-foreground/60 hover:text-foreground",
            )}
            activeProps={{
              className: "bg-background text-foreground hover:text-foreground",
            }}
          >
            {item.label}
          </Link>
        ))}
      </nav>
      <div className="min-w-0">
        <Outlet />
      </div>
    </div>
  );
}
