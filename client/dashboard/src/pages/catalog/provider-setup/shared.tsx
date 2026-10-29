import { useMock } from "./use-mock";
import { type ReactNode, type ComponentType } from "react";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Field, FieldError, FieldLabel } from "@/components/ui/Field";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import { ArrowUpRight, CirclePlus, SquareDashed } from "lucide-react";
import { type FieldName, names } from "./state";
import { cn } from "@/lib/utils";

export function Section({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <section className="mt-7 min-w-0 first:mt-0">
      <h3 className="mb-4 text-lg leading-snug font-normal">{title}</h3>
      {children}
    </section>
  );
}
export function InProvider({ actions }: { actions: ReactNode[] }): JSX.Element {
  const { s } = useMock();
  return (
    <div className="mt-[18px] border-l-2 border-[#2873d7] bg-[#f4f8ff] px-5 py-4 first:mt-0">
      <p className="mb-1.5 text-[13px] text-[#2873d7]">
        In {names[s.provider]}
      </p>
      {actions.length === 1 ? (
        <p className="leading-[1.65]">{actions[0]}</p>
      ) : (
        <ol className="list-inside list-decimal leading-[1.65]">
          {actions.map((action, i) => (
            <li key={i}>{action}</li>
          ))}
        </ol>
      )}
    </div>
  );
}
export function ExternalAction({
  title,
  children,
  label,
  href,
  disabled = false,
}: {
  title: string;
  children?: ReactNode;
  label: string;
  href?: string;
  disabled?: boolean;
}): JSX.Element {
  return (
    <div className="mt-6 flex flex-col items-start justify-between gap-6 bg-[#fff8f3] p-[22px] shadow-[inset_2px_0_#fb873f] first:mt-0 @min-[650px]/task:flex-row">
      <div className="min-w-0 flex-1">
        {title && (
          <h4 className="mb-2 pt-1.5 text-base font-semibold">{title}</h4>
        )}
        {children}
      </div>
      {href ? (
        <Button
          variant="primary"
          asChild
          className="self-end @min-[650px]/task:self-start"
        >
          <a href={href} target="_blank" rel="noopener noreferrer">
            {label}
            <ArrowUpRight className="ml-2 size-4" />
          </a>
        </Button>
      ) : (
        <Button
          variant="primary"
          disabled={disabled}
          className="self-end @min-[650px]/task:self-start"
        >
          <Button.Text>{label}</Button.Text>
          <Button.RightIcon>
            <ArrowUpRight />
          </Button.RightIcon>
        </Button>
      )}
    </div>
  );
}
export function MockField({
  field,
  label,
  placeholder,
  multiline = false,
  disabled = false,
  value,
  type,
}: {
  field: FieldName;
  label: string;
  placeholder?: string;
  multiline?: boolean;
  disabled?: boolean;
  value?: string;
  type?: string;
}): JSX.Element {
  const { s, dispatch } = useMock();
  const id = `mock-${s.provider}-${field}`,
    error = s.errors[field];
  const secret = field === "clientSecret";
  return (
    <Field className="mt-6" data-invalid={!!error}>
      <div className="flex items-baseline justify-between">
        <FieldLabel htmlFor={id}>{label}</FieldLabel>
        {secret && !disabled && (
          <Button
            variant="tertiary"
            size="xs"
            className="-mr-2 h-auto py-0 font-sans text-[13px]"
            aria-controls={id}
            aria-pressed={s.showSecret}
            onClick={() => dispatch({ type: "secret" })}
          >
            {s.showSecret ? "Hide" : "Show"}
          </Button>
        )}
      </div>
      <Input
        id={id}
        data-field={field}
        value={value ?? s.values[field]}
        placeholder={placeholder}
        noAutofill
        disabled={disabled}
        multiline={multiline}
        lines={multiline ? 3 : 1}
        type={
          secret
            ? s.showSecret && !disabled
              ? "text"
              : "password"
            : (type ?? "text")
        }
        error={!!error}
        aria-invalid={!!error}
        aria-describedby={error ? `${id}-error` : undefined}
        onChange={(value) => dispatch({ type: "field", field, value })}
      />
      {error && <FieldError id={`${id}-error`}>{error}</FieldError>}
    </Field>
  );
}
function ExistingAppIcon({ className }: { className?: string }): JSX.Element {
  return (
    <SquareDashed className={className}>
      <path d="M8 12h8M12 8v8" />
    </SquareDashed>
  );
}
export type ChoiceOption = {
  value: string;
  title: string;
  description?: string;
  icon: ComponentType<{ className?: string }>;
};
export function Choices({
  field,
  label,
  visibleLabel = true,
  options,
  className,
}: {
  field: FieldName;
  label: string;
  visibleLabel?: boolean;
  options: ChoiceOption[];
  className?: string;
}): JSX.Element {
  const { s, dispatch } = useMock();
  return (
    <fieldset className={cn("mb-7 min-w-0 border-0 p-0", className)}>
      {visibleLabel && (
        <legend className="mb-3 text-base font-semibold">{label}</legend>
      )}
      <RadioCardGroup
        aria-label={label}
        value={s.values[field]}
        showIndicator={false}
        className="grid grid-cols-1 gap-2 sm:grid-cols-2"
        onValueChange={(value) => dispatch({ type: "field", field, value })}
      >
        {options.map(({ value, title, description, icon: Icon }) => (
          <RadioCard
            key={value}
            value={value}
            title={
              <span className="text-lg leading-snug font-semibold">
                {title}
              </span>
            }
            leading={<Icon className="mt-0 size-6 shrink-0" />}
            className="gap-3 p-6 text-[#767676] has-data-[state=checked]:bg-[#fafafa] has-data-[state=checked]:text-[#101010]"
          >
            {description && (
              <span className="mt-2 block text-[13px] leading-normal">
                {description}
              </span>
            )}
          </RadioCard>
        ))}
      </RadioCardGroup>
    </fieldset>
  );
}
export function AppChoice({
  descriptions,
}: {
  descriptions?: [string, string];
}): JSX.Element {
  return (
    <Choices
      field="appChoice"
      label="OAuth app"
      visibleLabel={false}
      options={[
        {
          value: "new",
          title: "Create a new app",
          description: descriptions?.[0],
          icon: CirclePlus,
        },
        {
          value: "existing",
          title: "Use an existing app",
          description: descriptions?.[1],
          icon: ExistingAppIcon,
        },
      ]}
    />
  );
}
export function CopyValue({
  label,
  value,
  hideLabel = false,
}: {
  label: string;
  value: string;
  hideLabel?: boolean;
}): JSX.Element {
  const { s, dispatch } = useMock();
  return (
    <div className="mt-4">
      <label
        className={hideLabel ? "sr-only" : "mb-2 block"}
        htmlFor={`copy-${label.replace(/\W/g, "")}`}
      >
        {label}
      </label>
      <div className="flex items-stretch gap-2">
        <div className="min-w-0 flex-1">
          <Input
            id={`copy-${label.replace(/\W/g, "")}`}
            readOnly
            value={value}
          />
        </div>
        <Button
          variant="secondary"
          onClick={() => {
            void navigator.clipboard.writeText(value);
            dispatch({ type: "copy", key: label });
          }}
        >
          {s.copied === label ? "Copied" : "Copy"}
        </Button>
      </div>
    </div>
  );
}
export function Correction({
  step,
  children,
}: {
  step: string;
  children: ReactNode;
}): JSX.Element {
  const { dispatch } = useMock();
  return (
    <button
      type="button"
      className="cursor-pointer text-left underline underline-offset-4"
      onClick={() => dispatch({ type: "goto", step, correction: true })}
    >
      {children}
    </button>
  );
}
export function Note({ children }: { children: ReactNode }): JSX.Element {
  return (
    <div className="mt-6 border-l-2 bg-[#fafafa] px-5 py-4" role="status">
      {children}
    </div>
  );
}
