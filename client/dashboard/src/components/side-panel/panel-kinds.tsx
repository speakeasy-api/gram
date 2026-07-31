import { SetupGuidePanel } from "@/components/setup-guide/SetupGuidePanel";
import type { SidePanelDescriptor } from "./side-panel-context";

/**
 * The registry of everything the side panel can hold.
 *
 * A kind is reachable only from here, and the switch is exhaustive, so adding
 * a member to `SidePanelDescriptor` is a compile error until it is handled.
 */
export function SidePanelKind({
  descriptor,
}: {
  descriptor: SidePanelDescriptor;
}): React.JSX.Element {
  switch (descriptor.kind) {
    case "setup-guide":
      return <SetupGuidePanel {...descriptor.props} />;
  }
}
