import { useEffect } from "react";
import { useSearchParams } from "react-router";

export function LoginPage() {
  const [searchParams] = useSearchParams();
  const redirectPath = searchParams.get("redirect") || "/";

  useEffect(() => {
    // Check if already authenticated
    async function checkAndRedirect() {
      try {
        const res = await fetch("/rpc/auth.info", {
          method: "GET",
          credentials: "include",
        });

        if (res.ok) {
          // Already authenticated, redirect back
          window.location.href = redirectPath;
          return;
        }
      } catch {
        // Not authenticated, continue showing login page
      }
    }

    checkAndRedirect();
  }, [redirectPath]);

  function handleLogin() {
    // Redirect to the auth login endpoint (proxied through the chat domain).
    // The redirect parameter is a relative URL so the callback redirects back
    // to the correct path on this same origin after authentication.
    window.location.href = `/rpc/auth.login?redirect=${encodeURIComponent(redirectPath)}`;
  }

  return (
    <div className="flex h-full flex-col items-center justify-center bg-neutral-950 text-white">
      <div className="w-full max-w-sm rounded-xl border border-neutral-800 bg-neutral-900 p-8">
        <div className="mb-6 flex flex-col items-center">
          <svg
            width="40"
            height="40"
            viewBox="0 0 32 32"
            fill="none"
            xmlns="http://www.w3.org/2000/svg"
          >
            <rect width="32" height="32" rx="8" fill="#6366f1" />
            <text
              x="16"
              y="22"
              textAnchor="middle"
              fill="white"
              fontSize="18"
              fontWeight="bold"
              fontFamily="sans-serif"
            >
              G
            </text>
          </svg>
          <h1 className="mt-4 text-xl font-semibold">Gram Chat</h1>
          <p className="mt-1 text-sm text-neutral-400">
            Sign in to access your chat
          </p>
        </div>

        <button
          onClick={handleLogin}
          className="w-full rounded-lg bg-indigo-600 px-4 py-2.5 text-sm font-medium text-white transition-colors hover:bg-indigo-500"
        >
          Sign in with Gram
        </button>
      </div>
    </div>
  );
}
