import { useSearchParams } from "react-router";

const unexpected = "Server error. Please try again later or contact support.";
const authErrorMessages: Record<string, string> = {
  signin_link_invalid:
    "This sign-in link expired, was already used, or belongs to another browser. Please sign in again.",
  signin_canceled: "Sign-in was canceled. You can try again when you're ready.",
  lookup_error:
    "Failed to look up account details. Try again or contact support.",
  init_error: "Failed to initialize account. Try again or contact support.",
  sign_in_revoked: "Your sign-in was ended. Sign in again to continue.",
  // A session transfer between Speakeasy hosts failed and sent the browser here.
  transfer_session_expired:
    "Your previous sign-in is no longer available. Please sign in again.",
  transfer_wrong_destination:
    "We couldn't complete sign-in on this site. Please sign in here.",
  transfer_not_transferable:
    "This session can't be moved to another site. Please sign in here.",
  transfer_expired:
    "This sign-in transfer is no longer valid. Please sign in again.",
  transfer_browser_mismatch:
    "We couldn't complete the sign-in transfer in this browser. Please sign in here.",
  transfer_access_changed:
    "Your access has changed. Please sign in again; contact your administrator if you still cannot get in.",
  transfer_temporary_error:
    "We couldn't complete sign-in right now. Please try again.",
  unexpected,
};

function getAuthErrorMessage(errorCode?: string | null): string {
  if (!errorCode) {
    return unexpected;
  }
  return authErrorMessages[errorCode] || unexpected;
}

export function AuthErrorText({
  children,
}: {
  children: React.ReactNode;
}): JSX.Element {
  return (
    <p className="text-center text-[14px] text-[var(--vermilion)]">
      {children}
    </p>
  );
}

// IDP redirect errors arrive as a `signin_error` query param on both the
// login and register screens.
export function SigninErrorNotice(): JSX.Element | null {
  const [searchParams] = useSearchParams();
  const signinError = searchParams.get("signin_error");
  if (!signinError) {
    return null;
  }
  return <AuthErrorText>{getAuthErrorMessage(signinError)}</AuthErrorText>;
}
