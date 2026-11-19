import {
  acceptInspectCallback,
  INSPECT_OAUTH_CALLBACK,
} from "@/lib/inspect-oauth";
import { useEffect, useRef, useState } from "react";

export function InspectOAuthCallback(): JSX.Element {
  const started = useRef(false);
  const [failure, setFailure] = useState<{
    message: string;
    returnTo: string;
  }>();
  useEffect(() => {
    if (started.current) return;
    started.current = true;
    const result = acceptInspectCallback(new URLSearchParams(location.search));
    // Drop the single-use code from browser history before exchanging it.
    history.replaceState(history.state, "", INSPECT_OAUTH_CALLBACK);
    if (!result) {
      setFailure({
        message: "This Inspect sign-in has expired or isn't valid.",
        returnTo: "/",
      });
      return;
    }
    void result
      .exchange()
      .then((message) => {
        if (message) setFailure({ message, returnTo: result.returnTo });
        else location.replace(result.returnTo);
      })
      .catch(() =>
        setFailure({
          message: "Couldn't finish signing in.",
          returnTo: result.returnTo,
        }),
      );
  }, []);

  return (
    <main className="mx-auto flex min-h-screen max-w-lg flex-col justify-center gap-4 p-6">
      <h1 className="text-xl font-semibold">MCP Inspect sign-in</h1>
      {failure ? (
        <>
          <p role="alert">{failure.message}</p>
          <a className="underline" href={failure.returnTo}>
            Return to the dashboard
          </a>
        </>
      ) : (
        <p role="status">Finishing sign-in…</p>
      )}
    </main>
  );
}
