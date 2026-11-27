import { act, render } from "@testing-library/react";
import { StrictMode } from "react";
import { describe, expect, it, vi } from "vitest";

import { useOnUnmount } from "./useOnUnmount";

function Harness({ cleanup }: { cleanup: () => void }): null {
  useOnUnmount(cleanup);
  return null;
}

describe("useOnUnmount", () => {
  it("runs only the latest cleanup when the component unmounts", async () => {
    const first = vi.fn((): void => {});
    const latest = vi.fn((): void => {});
    const view = render(<Harness cleanup={first} />);

    expect(first).not.toHaveBeenCalled();
    view.rerender(<Harness cleanup={latest} />);
    expect(first).not.toHaveBeenCalled();
    expect(latest).not.toHaveBeenCalled();

    await act(async () => view.unmount());
    expect(latest).toHaveBeenCalledOnce();

    expect(first).not.toHaveBeenCalled();
  });

  it("ignores Strict Mode's effect replay", async () => {
    const cleanup = vi.fn((): void => {});
    const view = render(
      <StrictMode>
        <Harness cleanup={cleanup} />
      </StrictMode>,
    );

    await Promise.resolve();
    expect(cleanup).not.toHaveBeenCalled();

    await act(async () => view.unmount());
    expect(cleanup).toHaveBeenCalledOnce();
  });
});
