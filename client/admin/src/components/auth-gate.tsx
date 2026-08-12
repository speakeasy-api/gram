import type { JSX, ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";

import { SpeakeasyMark } from "@/components/speakeasy-mark";
import { adminSessionQuery, isRedirectingToLogin } from "@/lib/gramAdminApi";

// Holds the first paint until the session check answers.
//
// Every page fetches with credentials on mount, and gramAdminFetch redirects to
// /admin/auth.login on a 401. Without this gate an operator with no session
// sees the whole admin shell paint before the redirect.
export function AuthGate({ children }: { children: ReactNode }): JSX.Element {
  const session = useQuery(adminSessionQuery);

  // Only a login redirect holds the placeholder. Other failures render the
  // app, because each page reports its own errors.
  if (session.isPending || isRedirectingToLogin(session.error)) {
    return <Placeholder />;
  }

  return <>{children}</>;
}

// A session check that answers inside 500ms shows nothing at all. delay-500 is
// the tw-animate-css utility, so it delays the animation rather than a
// transition, and fill-mode-both holds the opening frame through the delay.
function Placeholder(): JSX.Element {
  return (
    <div className="flex h-full items-center justify-center">
      <div className="animate-in fade-in fill-mode-both delay-500 duration-300">
        <SpeakeasyMark className="size-10 animate-pulse text-muted-foreground" />
      </div>
    </div>
  );
}
