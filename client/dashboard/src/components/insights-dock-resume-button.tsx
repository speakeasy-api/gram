import {
  INSIGHTS_DOCK_CONTENT_VT_CLASS,
  INSIGHTS_DOCK_VT_CLASS,
  useInsightsDockCta,
} from "@/hooks/useInsightsDockCta";
import { Sparkles } from "lucide-react";
import { SidebarFooterAction } from "./sidebar-footer-action";

/**
 * Persistent entry point back to the docked Project Assistant composer, shown
 * as a standout item in the sidebar footer once the dock has been dismissed.
 * It shares a view-transition name with the dock, so dismissing the dock
 * genies it down into this button, and clicking the button genies it back
 * (see `useInsightsDockCta`).
 */
export function InsightsDockResumeButton(): JSX.Element | null {
  const { dismissed, resume } = useInsightsDockCta();

  if (!dismissed) return null;

  return (
    <SidebarFooterAction
      onClick={resume}
      icon={Sparkles}
      label="Project Assistant"
      className={INSIGHTS_DOCK_VT_CLASS}
      contentClassName={INSIGHTS_DOCK_CONTENT_VT_CLASS}
    />
  );
}
