import { buildLoginRedirectURL, cn } from "@/lib/utils";
import { AUTH_BUTTON_CLASSES, AUTH_PILLARS } from "./auth-constants";
import { SigninErrorNotice } from "./auth-errors";

export function LoginPanel({
  redirectTo,
}: {
  redirectTo: string | null;
}): JSX.Element {
  const handleLogin = () => {
    window.location.href = buildLoginRedirectURL(redirectTo);
  };

  return (
    <>
      <div className="text-center">
        <p className="text-[16px]">
          Securely scale AI usage across your organization.
        </p>
        <p className="mt-1.5 text-[14px] text-[var(--stone)]">
          Control plane to govern Agents, MCP and Skills.
        </p>
      </div>

      <div className="flex gap-2">
        {AUTH_PILLARS.map((label) => (
          <span
            key={label}
            className="auth-mono rounded-full border border-[var(--rule)] px-[11px] py-[5px] text-[11px]"
          >
            {label}
          </span>
        ))}
      </div>

      <SigninErrorNotice />

      <button
        onClick={handleLogin}
        className={cn(AUTH_BUTTON_CLASSES, "mt-2 w-[280px]")}
      >
        Log in
      </button>

      <p className="auth-mono-text text-center text-[11px] leading-relaxed tracking-[0.02em] text-[var(--stone)]">
        Single sign-on through your identity provider.
      </p>
    </>
  );
}
