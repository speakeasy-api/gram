import { useSearchParams } from "react-router";

const unexpected = "Server error. Please try again later or contact support.";
const authErrorMessages: Record<string, string> = {
  lookup_error:
    "Failed to look up account details. Try again or contact support.",
  init_error: "Failed to initialize account. Try again or contact support.",
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
