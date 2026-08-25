import { useTelemetry } from "@/contexts/Telemetry";
import { authPageHref } from "@/lib/safe-external-url";
import { buildLoginRedirectURL, cn } from "@/lib/utils";
import { useForm } from "@tanstack/react-form";
import { Link } from "react-router";
import { AUTH_BUTTON_CLASSES, AUTH_PILLARS } from "./auth-constants";
import { SigninErrorNotice } from "./auth-errors";
import { normalizeOrgName, validateOrgName } from "./org-name";

// Mirrors the server's validateSignupEmail. WorkOS verifies the address and
// the user can edit it on the next screen, so this only catches typos before
// the redirect.
const MAX_EMAIL_LENGTH = 254;

function validateEmail(value: string): string | undefined {
  const trimmed = value.trim();
  if (!trimmed) return "Email is required";
  if (trimmed.length > MAX_EMAIL_LENGTH) {
    return `Email must be ${MAX_EMAIL_LENGTH} characters or fewer`;
  }
  const parts = trimmed.split("@");
  if (parts.length !== 2 || !parts[0] || !parts[1] || /\s/.test(trimmed)) {
    return "Enter a valid email address";
  }
  return undefined;
}

/** First error string for a field, or undefined. */
function firstError(errors: unknown[]): string | undefined {
  const error = errors.find(Boolean);
  return typeof error === "string" ? error : undefined;
}

export function SignUpPanel({
  redirectTo,
}: {
  redirectTo?: string | null;
}): JSX.Element {
  const telemetry = useTelemetry();
  const form = useForm({
    defaultValues: { email: "", companyName: "" },
    onSubmit: ({ value }) => {
      // The server has no identity until the identity provider answers, so it
      // can't count this attempt — only the client sees it. Firing here joins
      // signup_started to the server's new_org_created for a real conversion rate.
      telemetry.capture("onboarding_event", {
        action: "signup_started",
        created_via: "signup",
      });

      // The org itself is created server-side during the auth callback. The
      // name rides on this one login request as a query param, is stashed
      // against the login nonce, and stops there: it is not carried through
      // the identity-provider round trip and is not on the URL the user lands
      // on afterwards. The email becomes WorkOS's login_hint so the field
      // arrives pre-filled, and is never stored at all.
      window.location.assign(
        buildLoginRedirectURL(
          redirectTo ?? null,
          normalizeOrgName(value.companyName),
          value.email.trim(),
        ),
      );
    },
  });

  return (
    <>
      <div className="text-center tracking-[0.0025em]">
        <p className="text-[16px]">
          Securely scale AI usage across your organization.
        </p>
        <p className="mt-1.5 text-[14px] text-(--muted-strong)">
          Control plane to govern Agents, MCP and Skills
        </p>
      </div>

      <div className="flex gap-2">
        {AUTH_PILLARS.map((label) => (
          <span
            key={label}
            className="auth-mono border border-(--edge) px-[11px] py-[5px] text-[11px]"
          >
            {label}
          </span>
        ))}
      </div>

      <form
        onSubmit={(e) => {
          e.preventDefault();
          void form.handleSubmit();
        }}
        className="mt-4 flex w-full flex-col gap-4"
      >
        <div className="flex flex-col gap-1">
          <p className="auth-mono text-[16px] tracking-[0.06em]">Sign up</p>
          <p className="auth-mono-text text-[12px] text-(--muted-strong)">
            Get a 14-day trial of the full control plane.
          </p>
        </div>

        {/* Field and CTA share one subscription: `form.state` is a plain getter
            onto the store, so only `Subscribe` (and `field.state` inside
            `Field`) re-renders when form state changes.

            Validators run on change and again on submit, so an untouched empty
            form starts error-free with the CTA enabled — the design's default
            state — and submitting it surfaces the required error rather than
            presenting a dead button. */}
        <form.Subscribe
          selector={(s) =>
            [s.canSubmit, s.isSubmitting, s.isSubmitted] as const
          }
        >
          {([canSubmit, isSubmitting, isSubmitted]) => (
            <>
              <form.Field
                name="email"
                validators={{
                  onChange: ({ value }) => validateEmail(value),
                  onSubmit: ({ value }) => validateEmail(value),
                }}
              >
                {(field) => {
                  const error = firstError(field.state.meta.errors);
                  return (
                    <div className="flex flex-col gap-1">
                      <label
                        htmlFor={field.name}
                        className="auth-mono text-[12px] text-(--muted-strong)"
                      >
                        Work email
                      </label>
                      <input
                        id={field.name}
                        name={field.name}
                        type="email"
                        value={field.state.value}
                        onChange={(e) => field.handleChange(e.target.value)}
                        onBlur={field.handleBlur}
                        placeholder="you@company.com"
                        aria-invalid={error ? true : undefined}
                        aria-describedby={
                          error ? `${field.name}-error` : undefined
                        }
                        className={cn(
                          "w-full border bg-(--card) px-3.5 py-[11px] text-[16px] text-black placeholder:text-(--muted) placeholder:opacity-55 focus:outline-none",
                          error
                            ? "border-destructive-default"
                            : "border-(--input-edge) focus:border-(--focus)",
                        )}
                      />
                      {error && (
                        <p
                          id={`${field.name}-error`}
                          className="mt-0.5 text-[12px] leading-[1.45] text-destructive"
                        >
                          {error}
                        </p>
                      )}
                    </div>
                  );
                }}
              </form.Field>

              <form.Field
                name="companyName"
                validators={{
                  onChange: ({ value }) => validateOrgName(value),
                  onSubmit: ({ value }) => validateOrgName(value),
                }}
              >
                {(field) => {
                  const error = firstError(field.state.meta.errors);
                  return (
                    <div className="flex flex-col gap-1">
                      <label
                        htmlFor={field.name}
                        className="auth-mono text-[12px] text-(--muted-strong)"
                      >
                        Company name
                      </label>
                      <input
                        id={field.name}
                        name={field.name}
                        type="text"
                        value={field.state.value}
                        onChange={(e) => field.handleChange(e.target.value)}
                        onBlur={field.handleBlur}
                        placeholder="Acme Inc"
                        aria-invalid={error ? true : undefined}
                        aria-describedby={
                          error ? `${field.name}-error` : undefined
                        }
                        className={cn(
                          "w-full border bg-(--card) px-3.5 py-[11px] text-[16px] text-black placeholder:text-(--muted) placeholder:opacity-55 focus:outline-none",
                          error
                            ? "border-destructive-default"
                            : "border-(--input-edge) focus:border-(--focus)",
                        )}
                      />
                      {error && (
                        <p
                          id={`${field.name}-error`}
                          className="mt-0.5 text-[12px] leading-[1.45] text-destructive"
                        >
                          {error}
                        </p>
                      )}
                    </div>
                  );
                }}
              </form.Field>

              {/* No spinner: the handoff is a top-level navigation, not an
                  awaited promise, so isSubmitting flips back before the browser
                  leaves the page and a spinner would flash for a few frames at
                  most.

                  isSubmitted keeps the button locked afterwards. Without it a
                  second click fires signup_started twice.

                  The label names no identity provider. The destination is
                  AuthKit's hosted page, which offers every method enabled in
                  WorkOS — email and password among them. */}
              <button
                type="submit"
                disabled={!canSubmit || isSubmitting || isSubmitted}
                className={cn(
                  AUTH_BUTTON_CLASSES,
                  "mx-auto px-[22px] disabled:cursor-not-allowed disabled:opacity-50",
                )}
              >
                Start Trial
              </button>
            </>
          )}
        </form.Subscribe>
      </form>

      {/* Auth failures return as ?signin_error=. The design places the notice
          below the CTA, not above the heading. */}
      <SigninErrorNotice />

      <p className="mt-2 text-[14px] text-(--muted-strong)">
        Already have an account?{" "}
        <Link
          to={authPageHref("/login", redirectTo)}
          className="text-(--link) underline hover:text-(--focus)"
        >
          Log in
        </Link>
      </p>
    </>
  );
}
