import {
  Component,
  lazy,
  Suspense,
  type ComponentProps,
  type JSX,
  type ReactNode,
} from "react";

import { Button } from "@/components/ui/button";
import type { Calendar } from "@/components/ui/calendar";

const LazyCalendar = lazy(async () => {
  const { Calendar } = await import("@/components/ui/calendar");
  return { default: Calendar };
});

class CalendarLoadBoundary extends Component<
  { children: ReactNode },
  { failed: boolean }
> {
  state = { failed: false };

  static getDerivedStateFromError(): { failed: boolean } {
    return { failed: true };
  }

  render(): ReactNode {
    if (!this.state.failed) return this.props.children;
    return (
      <div className="p-3">
        <p role="alert">Could not load the calendar.</p>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => window.location.reload()}
        >
          Reload page
        </Button>
      </div>
    );
  }
}

// Mount inside the popover: importing a route or opening its enclosing dialog
// must not load the date picker. Keep the vendored calendar itself unchanged.
export function DeferredCalendar(
  props: ComponentProps<typeof Calendar>,
): JSX.Element {
  return (
    <CalendarLoadBoundary>
      <Suspense
        fallback={
          <p className="p-3" role="status">
            Loading calendar…
          </p>
        }
      >
        <LazyCalendar {...props} />
      </Suspense>
    </CalendarLoadBoundary>
  );
}
