import { buildLoginRedirectURL, cn } from "@/lib/utils";
import { useForm } from "@tanstack/react-form";
import { Link } from "react-router";
import { AUTH_BUTTON_CLASSES, AUTH_PILLARS } from "./auth-constants";
import { SigninErrorNotice } from "./auth-errors";

const VALID_ORG_NAME_REGEX = /^[a-zA-Z0-9\s-_]+$/;
const INVALID_ORG_NAME_MESSAGE =
  "Company name contains invalid characters. Only letters, numbers, spaces, hyphens, and underscores are allowed.";

// Deliberately loose: the authoritative address comes back from the identity
// provider, so this only catches obvious typos in the typed value.
const EMAIL_REGEX = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

function validateFullName(value: string): string | undefined {
  return value.trim() ? undefined : "Full name is required";
}

function validateEmail(value: string): string | undefined {
  if (!value.trim()) return "Work email is required";
  return EMAIL_REGEX.test(value.trim())
    ? undefined
    : "Enter a valid email address";
}

function validateCompanyName(value: string): string | undefined {
  if (!value.trim()) return "Company name is required";
  return VALID_ORG_NAME_REGEX.test(value)
    ? undefined
    : INVALID_ORG_NAME_MESSAGE;
}

// Mono uppercase label + input + inline error, per the sign up design. The
// register panel's single field is styled inline; there are three here, so it
// gets a component rather than three copies of the same class string.
function SignUpField({
  id,
  label,
  type,
  placeholder,
  value,
  error,
  disabled,
  onChange,
  onBlur,
}: {
  id: string;
  label: string;
  type: "text" | "email";
  placeholder: string;
  value: string;
  error?: string;
  disabled: boolean;
  onChange: (value: string) => void;
  onBlur: () => void;
}): JSX.Element {
  return (
    <div className="flex flex-col gap-1">
      <label
        htmlFor={id}
        className="auth-mono text-[12px] text-(--muted-strong)"
      >
        {label}
      </label>
      <input
        id={id}
        name={id}
        type={type}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onBlur={onBlur}
        placeholder={placeholder}
        disabled={disabled}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? `${id}-error` : undefined}
        className={cn(
          "w-full rounded-md border bg-(--card) px-3.5 py-[11px] text-[16px] text-black placeholder:text-(--muted) placeholder:opacity-55 focus:outline-none",
          error
            ? "border-(--vermilion)"
            : "border-(--input-edge) focus:border-(--focus)",
        )}
      />
      {error && (
        <p
          id={`${id}-error`}
          className="mt-0.5 text-[12px] leading-[1.45] text-(--vermilion)"
        >
          {error}
        </p>
      )}
    </div>
  );
}

/** First error string for a field, or undefined. */
function firstError(errors: unknown[]): string | undefined {
  const error = errors.find(Boolean);
  return typeof error === "string" ? error : undefined;
}

export function SignUpPanel(): JSX.Element {
  const form = useForm({
    defaultValues: { fullName: "", email: "", companyName: "" },
    onSubmit: () => {
      // Account and org creation happen after the identity provider
      // round-trip, on /register. The typed name and email are not persisted
      // yet — the register endpoint only accepts an org name.
      window.location.href = buildLoginRedirectURL("/register");
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
            className="auth-mono rounded-full border border-(--edge) px-[11px] py-[5px] text-[11px]"
          >
            {label}
          </span>
        ))}
      </div>

      <SigninErrorNotice />

      <form
        onSubmit={(e) => {
          e.preventDefault();
          void form.handleSubmit();
        }}
        className="mt-4 flex w-full flex-col gap-6"
      >
        <div className="flex flex-col gap-1">
          <p className="auth-mono text-[16px] tracking-[0.06em]">Sign up</p>
          <p className="auth-mono-text text-[12px] text-(--muted-strong)">
            Get a 14-day trial of the full control plane.
          </p>
        </div>

        {/* Fields and CTA share one subscription: `form.state` is a plain
            getter onto the store, so only `Subscribe` (and `field.state`
            inside `Field`) re-renders when the form state changes.

            Validators run on change and again on submit, so an untouched empty
            form starts error-free with the CTA enabled — the design's default
            state — and submitting it surfaces the required errors. */}
        <form.Subscribe
          selector={(s) => [s.canSubmit, s.isSubmitting] as const}
        >
          {([canSubmit, isSubmitting]) => (
            <>
              <form.Field
                name="fullName"
                validators={{
                  onChange: ({ value }) => validateFullName(value),
                  onSubmit: ({ value }) => validateFullName(value),
                }}
              >
                {(field) => (
                  <SignUpField
                    id={field.name}
                    label="Full name"
                    type="text"
                    placeholder="Sarah Chen"
                    value={field.state.value}
                    error={firstError(field.state.meta.errors)}
                    disabled={isSubmitting}
                    onChange={field.handleChange}
                    onBlur={field.handleBlur}
                  />
                )}
              </form.Field>

              <form.Field
                name="email"
                validators={{
                  onChange: ({ value }) => validateEmail(value),
                  onSubmit: ({ value }) => validateEmail(value),
                }}
              >
                {(field) => (
                  <SignUpField
                    id={field.name}
                    label="Work email"
                    type="email"
                    placeholder="sarah@acme.com"
                    value={field.state.value}
                    error={firstError(field.state.meta.errors)}
                    disabled={isSubmitting}
                    onChange={field.handleChange}
                    onBlur={field.handleBlur}
                  />
                )}
              </form.Field>

              <form.Field
                name="companyName"
                validators={{
                  onChange: ({ value }) => validateCompanyName(value),
                  onSubmit: ({ value }) => validateCompanyName(value),
                }}
              >
                {(field) => (
                  <SignUpField
                    id={field.name}
                    label="Company name"
                    type="text"
                    placeholder="Acme Inc"
                    value={field.state.value}
                    error={firstError(field.state.meta.errors)}
                    disabled={isSubmitting}
                    onChange={field.handleChange}
                    onBlur={field.handleBlur}
                  />
                )}
              </form.Field>

              <button
                type="submit"
                disabled={!canSubmit}
                className={cn(
                  AUTH_BUTTON_CLASSES,
                  "mt-1 w-full disabled:cursor-not-allowed disabled:opacity-50",
                )}
              >
                {isSubmitting ? "Creating account" : "Start trial"}
              </button>
            </>
          )}
        </form.Subscribe>
      </form>

      <p className="text-[14px] text-(--muted-strong)">
        Already have an account?{" "}
        <Link
          to="/login"
          className="text-(--link) underline hover:text-(--focus)"
        >
          Log in
        </Link>
      </p>
    </>
  );
}
