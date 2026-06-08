import { Component, type ReactNode } from "react";

/**
 * Minimal error boundary for command-palette resource groups. Each group fetches
 * independently; if one endpoint errors (e.g. a permissions edge case), we drop
 * just that group rather than blanking the entire palette. Renders nothing on
 * failure — the group simply doesn't appear.
 */
export class CommandErrorBoundary extends Component<
  { children: ReactNode },
  { hasError: boolean }
> {
  constructor(props: { children: ReactNode }) {
    super(props);
    this.state = { hasError: false };
  }

  static getDerivedStateFromError(): { hasError: boolean } {
    return { hasError: true };
  }

  render(): ReactNode {
    if (this.state.hasError) return null;
    return this.props.children;
  }
}
