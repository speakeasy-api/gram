import { SetupGuidePanel } from "@/components/setup-guide/SetupGuidePanel";
import {
  SourceDetailPanel,
  SourceDownloadButton,
} from "@/components/sources/SourceDetailPanel";
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
    case "source":
      return <SourceDetailPanel {...descriptor.props} />;
  }
}

/**
 * A kind's own action in the panel header, beside Docs and Close.
 *
 * Kept next to the body registry so a kind's header and body are declared in
 * one place; kinds without an action return null.
 */
export function SidePanelKindHeaderAction({
  descriptor,
}: {
  descriptor: SidePanelDescriptor;
}): React.JSX.Element | null {
  switch (descriptor.kind) {
    case "setup-guide":
      return null;
    case "source":
      return <SourceDownloadButton {...descriptor.props} />;
  }
}
