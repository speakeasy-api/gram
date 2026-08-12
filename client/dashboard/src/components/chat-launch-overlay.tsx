import type { ReactElement } from "react";
import { Loader2 } from "lucide-react";
import {
  CHAT_LAUNCH_ADORNMENT_ATTR,
  CHAT_LAUNCH_CARD_ATTR,
  CHAT_LAUNCH_TEXT_ATTR,
  CHAT_LAUNCH_VEIL_ATTR,
  useChatLaunchState,
} from "@/lib/chat-launch";
import { INSIGHTS_SUGGESTION_ICONS } from "@/lib/insights-suggestions";

/**
 * The mid-flight state of a suggestion launch: the clicked chip, scaled up and
 * centred over a dimmed page, spinning a loader until the conversation's user
 * bubble exists. Mounted once in <AppLayout> so it outlives the navigation
 * into the chat route.
 *
 * Every part is addressed by data attribute rather than a ref, because
 * `useChatLaunch` drives the animation imperatively from outside React (see
 * that module for the two halves of the flight).
 */
export function ChatLaunchOverlay(): ReactElement | null {
  const launch = useChatLaunchState();
  if (!launch) return null;

  const SuggestionIcon = INSIGHTS_SUGGESTION_ICONS[launch.icon];

  return (
    <div
      role="status"
      aria-live="polite"
      className="fixed inset-0 z-[100] flex items-center justify-center px-6"
    >
      {/* Deliberately NOT given a view-transition-name. A named element is
          lifted into its own transition group, and groups paint in name order
          rather than DOM order — the veil would sit on top of the card for the
          whole flight and wash it out. Unnamed, it rides in the root snapshot,
          which swaps instantly, and every named group paints above it. */}
      <div
        aria-hidden="true"
        {...{ [CHAT_LAUNCH_VEIL_ATTR]: "" }}
        className="absolute inset-0"
      >
        <div className="bg-background/80 absolute inset-0 backdrop-blur-[2px]" />
      </div>
      <div
        {...{ [CHAT_LAUNCH_CARD_ATTR]: "" }}
        className="border-border bg-card text-foreground relative flex max-w-xl items-center gap-3 border px-6 py-4 text-lg shadow-lg"
      >
        <SuggestionIcon
          {...{ [CHAT_LAUNCH_ADORNMENT_ATTR]: "" }}
          className="size-5 shrink-0"
        />
        <span {...{ [CHAT_LAUNCH_TEXT_ATTR]: "" }} className="min-w-0">
          {launch.title}
        </span>
        <Loader2
          {...{ [CHAT_LAUNCH_ADORNMENT_ATTR]: "" }}
          className="text-muted-foreground size-4 shrink-0 animate-spin"
        />
      </div>
    </div>
  );
}
