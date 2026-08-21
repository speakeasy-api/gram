export type MockPylon = typeof window.Pylon & {
  emitShow: () => void;
  emitHide: () => void;
};

/** Test double that records onShow/onHide and fires them from show/hide. */
export function installMockPylon(): MockPylon {
  let onShow: (() => void) | null = null;
  let onHide: (() => void) | null = null;

  const pylon = Object.assign(
    (action: string, ...args: unknown[]) => {
      if (action === "onShow" && typeof args[0] === "function") {
        onShow = args[0] as () => void;
      }
      if (action === "onHide" && typeof args[0] === "function") {
        onHide = args[0] as () => void;
      }
      if (action === "show") {
        onShow?.();
      }
      if (action === "hide") {
        onHide?.();
      }
    },
    {
      q: [] as unknown[],
      e: () => undefined,
      emitShow: () => {
        onShow?.();
      },
      emitHide: () => {
        onHide?.();
      },
    },
  ) as MockPylon;

  window.Pylon = pylon;
  return pylon;
}
