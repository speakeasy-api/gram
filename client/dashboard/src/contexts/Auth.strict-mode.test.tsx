import { act, cleanup, render, screen } from "@testing-library/react";
import { StrictMode, useEffect, useState } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { SessionTokenStore } from "@/lib/session-token";

afterEach(cleanup);

it("survives StrictMode canceling bootstrap before the shared refresh completes", async () => {
  let finishRefresh!: (response: Response) => void;
  const pendingRefresh = new Promise<Response>((resolve) => {
    finishRefresh = resolve;
  });
  const fetcher = vi
    .fn<typeof fetch>()
    .mockReturnValueOnce(pendingRefresh)
    .mockImplementation(async (input) => {
      const request = input as Request;
      request.signal.throwIfAborted();
      return new Response(null, {
        status: 200,
        headers: { "Gram-Session": "access-1" },
      });
    });
  const store = new SessionTokenStore(
    () => "https://app.example.test",
    fetcher,
  );

  function Bootstrap() {
    const [status, setStatus] = useState("pending");
    useEffect(() => {
      const controller = new AbortController();
      void store
        .fetch("https://app.example.test/rpc/auth.info", {
          signal: controller.signal,
        })
        .then(() => {
          setStatus("authenticated");
        })
        .catch(() => {
          if (!controller.signal.aborted) setStatus("error");
        });
      return () => {
        controller.abort();
      };
    }, []);
    return <div>{status}</div>;
  }

  render(
    <StrictMode>
      <Bootstrap />
    </StrictMode>,
  );
  expect(fetcher).toHaveBeenCalledExactlyOnceWith(
    "https://app.example.test/rpc/auth.refresh",
    {
      method: "POST",
      credentials: "include",
      signal: expect.any(AbortSignal),
    },
  );
  await act(async () => {
    finishRefresh(
      new Response(null, {
        status: 204,
        headers: { "Gram-Session": "access-1" },
      }),
    );
  });
  expect(await screen.findByText("authenticated")).toBeTruthy();
  // StrictMode's canceled bootstrap never reaches the network.
  expect(fetcher).toHaveBeenCalledTimes(2);
  expect((fetcher.mock.calls[1]![0] as Request).signal.aborted).toBe(false);
});
