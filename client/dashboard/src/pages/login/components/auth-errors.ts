const unexpected = "Server error. Please try again later or contact support.";
const authErrorMessages: Record<string, string> = {
  lookup_error:
    "Failed to look up account details. Try again or contact support.",
  init_error: "Failed to initialize account. Try again or contact support.",
  unexpected,
};

export function getAuthErrorMessage(errorCode?: string | null): string {
  if (!errorCode) {
    return unexpected;
  }
  return authErrorMessages[errorCode] || unexpected;
}
