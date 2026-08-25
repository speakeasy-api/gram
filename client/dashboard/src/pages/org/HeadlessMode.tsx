import { ModeSurface } from "@/components/mode-switch-stage";
import { ModeSwitcher } from "@/components/mode-switcher";
import { HeadlessContent } from "./HeadlessContent";

/**
 * Headless mode: the app shell without sidebar or workspace header — only the
 * mode tab strip over the Platform MCP setup surface.
 */
export default function HeadlessMode(): JSX.Element {
  return (
    <div className="flex h-screen w-full flex-col">
      <ModeSwitcher mode="headless" />
      {/* No background of its own: the chrome-wide mesh shows through, so the
          strip and the hero read as one surface. */}
      <ModeSurface mode="headless" className="flex-1 overflow-auto">
        <HeadlessContent />
      </ModeSurface>
    </div>
  );
}
