import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { useOnUnmount } from "./useOnUnmount";

function Harness({ cleanup }: { cleanup: () => void }): null {
  useOnUnmount(cleanup);
  return null;
}

describe("useOnUnmount", () => {
  it("runs only the latest cleanup when the component unmounts", () => {
    const first = vi.fn((): void => {});
    const latest = vi.fn((): void => {});
    const view = render(<Harness cleanup={first} />);

    expect(first).not.toHaveBeenCalled();
    view.rerender(<Harness cleanup={latest} />);
    expect(first).not.toHaveBeenCalled();
    expect(latest).not.toHaveBeenCalled();

    view.unmount();

    expect(first).not.toHaveBeenCalled();
    expect(latest).toHaveBeenCalledOnce();
  });
});
