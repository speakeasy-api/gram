import { useEffect, useState } from "react";
import _Cal from "@calcom/embed-react";
import { Button } from "@speakeasy-api/moonshine";
import { useSessionData } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import { cn } from "@/lib/utils";
import {
  buildCalLink,
  DemoFormData,
  DemoFormErrors,
  PRODUCT_OPTIONS,
  splitDisplayName,
  validateDemoForm,
} from "./demo-booking";

// Cal's .d.ts returns the legacy global `JSX.Element`, incompatible with
// react-jsx/TS5. Widen only the return type; keep prop types intact so a
// mistyped prop (e.g. calLink) is still caught.
type CalProps = Parameters<typeof _Cal>[0];
const Cal = _Cal as unknown as (props: CalProps) => React.ReactElement | null;

type Step = "form" | "booking";

const inputStyles = cn(
  "w-full rounded-md border px-3 py-2.5 text-sm text-gray-900 transition-colors outline-none",
  "border-gray-300 bg-white placeholder:text-gray-400 focus:border-gray-500",
);

const labelStyles = "mb-0.5 text-sm text-gray-700";
const errorStyles = "mt-0.5 text-xs text-red-500";

export function DemoBookingFlow() {
  const { session } = useSessionData();
  const telemetry = useTelemetry();
  const [step, setStep] = useState<Step>("form");

  const [formData, setFormData] = useState<DemoFormData>(() => {
    const { firstName, lastName } = splitDisplayName(session?.user.displayName);
    return {
      firstName,
      lastName,
      email: session?.user.email ?? "",
      referralSource: "",
      product: "AI Control Plane",
    };
  });
  const [errors, setErrors] = useState<DemoFormErrors>({});

  useEffect(() => {
    if (step !== "booking") return;
    const handler = (e: MessageEvent) => {
      try {
        const data =
          typeof e.data === "string"
            ? (JSON.parse(e.data) as Record<string, unknown>)
            : (e.data as Record<string, unknown>);
        if (
          data?.originator === "CAL" &&
          (data?.fullType as string)?.endsWith("bookingSuccessful")
        ) {
          telemetry.capture("booked_demo", {
            first_name: formData.firstName.trim(),
            last_name: formData.lastName.trim(),
            email: formData.email.trim(),
            referral_source: formData.referralSource.trim(),
            product: formData.product,
          });
        }
      } catch {
        // ignore non-JSON postMessages from other senders
      }
    };
    window.addEventListener("message", handler);
    return () => window.removeEventListener("message", handler);
  }, [step, formData, telemetry]);

  const handleChange =
    (field: keyof DemoFormData) => (e: React.ChangeEvent<HTMLInputElement>) => {
      const value = e.target.value;
      setFormData((prev) => ({ ...prev, [field]: value }));
      setErrors((prev) => {
        if (!prev[field]) return prev;
        const next = { ...prev };
        delete next[field];
        return next;
      });
    };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const nextErrors = validateDemoForm(formData);
    setErrors(nextErrors);
    if (Object.keys(nextErrors).length > 0) return;
    telemetry.capture("demo_form_submitted", { product: formData.product });
    setStep("booking");
  };

  if (step === "booking") {
    return (
      <div className="flex w-full flex-col gap-3">
        <button
          type="button"
          onClick={() => setStep("form")}
          className="self-start text-sm text-[#8B8684] transition-colors hover:text-slate-600"
        >
          &larr; Back
        </button>
        <div className="h-[70vh] min-h-[520px] w-full overflow-auto">
          <Cal
            calLink={buildCalLink(formData)}
            config={{
              layout: "month_view",
              theme: "light",
              hideEventTypeDetails: "true",
              cssVarsPerTheme: JSON.stringify({
                light: { "cal-bg": "transparent" },
                dark: { "cal-bg": "transparent" },
              }),
            }}
            style={{ width: "100%", height: "100%", overflow: "auto" }}
          />
        </div>
      </div>
    );
  }

  return (
    <div className="flex w-full flex-col gap-4">
      <h2 className="text-lg font-semibold text-gray-900">
        Talk to our experts
      </h2>
      <form onSubmit={handleSubmit} className="flex flex-col gap-4" noValidate>
        <div className="flex flex-col gap-2.5 md:flex-row">
          <div className="flex w-full flex-col">
            <label htmlFor="demo-firstName" className={labelStyles}>
              First name <span className="text-red-500">*</span>
            </label>
            <input
              id="demo-firstName"
              type="text"
              autoComplete="given-name"
              value={formData.firstName}
              onChange={handleChange("firstName")}
              placeholder="Jane"
              className={cn(inputStyles, errors.firstName && "border-red-300")}
            />
            {errors.firstName && (
              <span className={errorStyles}>{errors.firstName}</span>
            )}
          </div>
          <div className="flex w-full flex-col">
            <label htmlFor="demo-lastName" className={labelStyles}>
              Last name <span className="text-red-500">*</span>
            </label>
            <input
              id="demo-lastName"
              type="text"
              autoComplete="family-name"
              value={formData.lastName}
              onChange={handleChange("lastName")}
              placeholder="Smith"
              className={cn(inputStyles, errors.lastName && "border-red-300")}
            />
            {errors.lastName && (
              <span className={errorStyles}>{errors.lastName}</span>
            )}
          </div>
        </div>

        <div className="flex flex-col gap-2.5 md:flex-row">
          <div className="flex w-full flex-col">
            <label htmlFor="demo-email" className={labelStyles}>
              Work email <span className="text-red-500">*</span>
            </label>
            <input
              id="demo-email"
              type="email"
              autoComplete="email"
              value={formData.email}
              onChange={handleChange("email")}
              placeholder="jane@example.com"
              className={cn(inputStyles, errors.email && "border-red-300")}
            />
            {errors.email && (
              <span className={errorStyles}>{errors.email}</span>
            )}
          </div>
          <div className="flex w-full flex-col">
            <label htmlFor="demo-referralSource" className={labelStyles}>
              How did you hear about us? <span className="text-red-500">*</span>
            </label>
            <input
              id="demo-referralSource"
              type="text"
              value={formData.referralSource}
              onChange={handleChange("referralSource")}
              placeholder="e.g. Google, Twitter, a friend"
              className={cn(
                inputStyles,
                errors.referralSource && "border-red-300",
              )}
            />
            {errors.referralSource && (
              <span className={errorStyles}>{errors.referralSource}</span>
            )}
          </div>
        </div>

        <fieldset className="flex flex-col gap-1.5">
          <legend className={labelStyles}>
            What are you interested in? <span className="text-red-500">*</span>
          </legend>
          <div className="grid grid-cols-2 gap-1.5">
            {PRODUCT_OPTIONS.map((option) => (
              <label
                key={option}
                className="flex cursor-pointer items-center gap-2"
              >
                <input
                  type="radio"
                  name="product"
                  value={option}
                  checked={formData.product === option}
                  onChange={handleChange("product")}
                  className="h-4 w-4 accent-gray-900"
                />
                <span className="text-sm text-gray-700">{option}</span>
              </label>
            ))}
          </div>
        </fieldset>

        <Button type="submit" variant="brand" className="mt-2 w-fit">
          Continue to booking
        </Button>
      </form>
    </div>
  );
}
