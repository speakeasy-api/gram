import { buildLoginRedirectURL, cn } from "@/lib/utils";
import { Link } from "react-router";
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
      <div className="text-center tracking-[0.0025em]">
        <p className="text-[16px]">
          Securely scale AI usage across your organization.
        </p>
        <p className="mt-1.5 text-[14px] text-[var(--muted-strong)]">
          Control plane to govern Agents, MCP and Skills
        </p>
      </div>

      <div className="flex gap-2">
        {AUTH_PILLARS.map((label) => (
          <span
            key={label}
            className="auth-mono border border-[var(--edge)] px-[11px] py-[5px] text-[11px]"
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

      <p className="auth-mono-text text-center text-[11px] leading-relaxed tracking-[0.02em] text-[var(--muted)]">
        Single sign-on through your identity provider.
      </p>

      <p className="mt-2 text-[14px] text-(--muted-strong)">
        Don't have an account?{" "}
        <Link
          to="/sign-up"
          className="text-(--link) underline hover:text-(--focus)"
        >
          Sign up
        </Link>
      </p>
    </>
  );
}
