import type { PublishStatusResult } from "@gram/client/models/components/publishstatusresult.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { MarketplaceCard } from "./MarketplaceCard";

afterEach(cleanup);

function renderOpenCard(publishStatus: PublishStatusResult): void {
  render(
    <MarketplaceCard
      publishStatus={publishStatus}
      onSync={() => {}}
      pausedPluginNames={["Paused"]}
    />,
  );
  fireEvent.click(
    screen.getByRole("button", { name: "Toggle project marketplace details" }),
  );
}

describe("MarketplaceCard sync copy", () => {
  it("keeps unavailable freshness distinct from an up-to-date marketplace", () => {
    renderOpenCard({ configured: true, connected: true });
    expect(screen.getByText("Sync status unavailable")).toBeTruthy();
    expect(screen.queryByText("Up to date")).toBeNull();
    expect(screen.queryByText("Marketplace set up")).toBeNull();
    expect(
      screen.getByText(
        "Sync status is unavailable. Check again shortly or sync changes.",
      ),
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: "Sync changes" })).toBeTruthy();
  });

  it("shows up to date only for confirmed current freshness", () => {
    renderOpenCard({ configured: true, connected: true, upToDate: true });
    expect(screen.getByText("Up to date")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Sync changes" })).toBeNull();
  });

  it("does not promise an automatic sync when only plugins with auto sync off are waiting", () => {
    renderOpenCard({
      configured: true,
      connected: true,
      upToDate: false,
      automaticSyncPending: false,
    });
    expect(screen.queryByText(/sync automatically/)).toBeNull();
    expect(
      screen.getByText(
        "Not up to date. The waiting changes belong to plugins with auto sync off, so they publish only when you click 'Sync changes'",
      ),
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: "Sync changes" })).toBeTruthy();
  });

  it("says changes sync automatically while an automatic publish is pending", () => {
    renderOpenCard({
      configured: true,
      connected: true,
      upToDate: false,
      automaticSyncPending: true,
    });
    expect(
      screen.getByText(
        "Not up to date yet. Changes sync automatically, or click 'Sync changes' to publish now",
      ),
    ).toBeTruthy();
  });
});
