import { ModeSurface } from "@/components/mode-switch-stage";
import { ModeSwitcher } from "@/components/mode-switcher";
import { HeadlessContent } from "./HeadlessContent";

/**
 * Headless mode: the app shell without sidebar or workspace header, with the
 * mode switcher overlaid on the Platform MCP setup surface.
 */
export default function HeadlessMode(): JSX.Element {
  return (
    // data-headless-mode drops the document's reserved scrollbar gutter (see
    // App.css): this page scrolls in its own pane, so the gutter sat empty.
    <div
      data-headless-mode=""
      className="bg-surface-tertiary-fixed-dark relative flex h-screen w-full flex-col"
    >
      <ModeSwitcher mode="headless" />
      {/* The pane paints the ink itself; the switcher floats above its hero. */}
      <ModeSurface mode="headless" className="flex-1 overflow-auto">
        <HeadlessContent />
      </ModeSurface>
    </div>
  );
}
