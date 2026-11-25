import { afterEach, describe, expect, it } from "vitest";
import { act, cleanup, renderHook } from "@testing-library/react";
import type { OktaResourceConnectionServer } from "@gram/client/models/components/oktaresourceconnectionserver.js";

import {
  sharedConfirmationKey,
  useClearedConfirmations,
} from "./useClearedConfirmations";
import { confirmedRow, pendingRow } from "./xaaTestRows";

const OTHER_ISSUER = "00000000-0000-4000-8000-000000000002";
const APPS = [{ id: "recorded-app", label: "Recorded app" }];

type Props = {
  servers: OktaResourceConnectionServer[];
  isPlaceholderData: boolean;
};

function renderSnapshots(
  servers: OktaResourceConnectionServer[] = [],
  apps = APPS,
) {
  return renderHook((props: Props) => useClearedConfirmations(props, apps), {
    initialProps: { servers, isPlaceholderData: false },
  });
}

afterEach(cleanup);

describe("sharedConfirmationKey", () => {
  it("groups servers by issuer ID and resource, else by server", () => {
    expect(sharedConfirmationKey(pendingRow(0))).toBe(
      sharedConfirmationKey(pendingRow(1)),
    );
    expect(sharedConfirmationKey(pendingRow(0))).not.toBe(
      sharedConfirmationKey({ ...pendingRow(1), issuerId: OTHER_ISSUER }),
    );
    expect(
      sharedConfirmationKey({ ...pendingRow(0), issuerId: undefined }),
    ).not.toBe(
      sharedConfirmationKey({ ...pendingRow(1), issuerId: undefined }),
    );
  });
});

describe("useClearedConfirmations", () => {
  it("keeps one snapshot per server, replacing an earlier one", () => {
    const { result } = renderSnapshots();
    act(() => result.current.remember(confirmedRow(0)));
    act(() => result.current.remember(confirmedRow(1)));
    act(() =>
      result.current.remember(
        confirmedRow(0, { audience: "https://issuer.example.com/newer" }),
      ),
    );
    expect(result.current.snapshots.map((s) => s.mcpServerId)).toEqual([
      "server-1",
      "server-0",
    ]);
    expect(result.current.snapshotFor("server-0")?.audience).toBe(
      "https://issuer.example.com/newer",
    );
    expect(result.current.snapshotFor("server-2")).toBeUndefined();
  });

  it("retires every snapshot sharing a confirmed row's issuer and resource", () => {
    const { result } = renderSnapshots();
    act(() => result.current.remember(confirmedRow(0)));
    act(() =>
      result.current.remember(confirmedRow(1, { issuerId: OTHER_ISSUER })),
    );
    act(() => result.current.retire([pendingRow(5)]));
    expect(result.current.snapshots.map((s) => s.mcpServerId)).toEqual([
      "server-1",
    ]);
  });

  it("retires on fresh connected readiness but not on placeholder data", () => {
    const { result, rerender } = renderSnapshots();
    act(() => result.current.remember(confirmedRow(0)));
    rerender({ servers: [confirmedRow(1)], isPlaceholderData: true });
    expect(result.current.snapshots).toHaveLength(1);
    rerender({ servers: [pendingRow(1)], isPlaceholderData: false });
    expect(result.current.snapshots).toHaveLength(1);
    rerender({ servers: [confirmedRow(1)], isPlaceholderData: false });
    expect(result.current.snapshots).toHaveLength(0);
  });

  it("allows Undo only with a saved audience and an available or absent app", () => {
    const { result } = renderSnapshots();
    expect(result.current.canUndo(confirmedRow(0))).toBe(true);
    expect(
      result.current.canUndo(confirmedRow(0, { oktaApplicationId: undefined })),
    ).toBe(true);
    expect(
      result.current.canUndo(confirmedRow(0, { oktaApplicationId: "gone" })),
    ).toBe(false);
    expect(result.current.canUndo(confirmedRow(0, { audience: "" }))).toBe(
      false,
    );
  });
});
