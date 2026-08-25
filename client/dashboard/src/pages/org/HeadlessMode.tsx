import { ModeSurface } from "@/components/mode-switch-stage";
import { ModeSwitcher } from "@/components/mode-switcher";
import { HeadlessContent } from "./HeadlessContent";

/**
 * Headless mode: the app shell without sidebar or workspace header — only the
 * mode tab strip over the Platform MCP setup surface.
 */
export default function HeadlessMode(): JSX.Element {
  return (
    <div className="bg-surface-tertiary-fixed-dark flex h-screen w-full flex-col">
      <ModeSwitcher mode="headless" />
      {/* The pane paints the ink itself so the strip and the hero read as one
          dark surface; the starfield lives inside the content. */}
      <ModeSurface mode="headless" className="flex-1 overflow-auto">
        <HeadlessContent />
      </ModeSurface>
    </div>
  );
}
