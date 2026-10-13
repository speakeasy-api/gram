export function PartnerCodeField({
  value,
  onChange,
  disabled,
}: {
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
}): JSX.Element {
  const tooLong = value.length > 80;
  return (
    <div className="flex w-full flex-col gap-2.5">
      <label
        htmlFor="partnerCode"
        className="auth-mono text-[12px] text-(--muted-strong)"
      >
        Partner code (optional)
      </label>
      <input
        id="partnerCode"
        name="partnerCode"
        type="text"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        disabled={disabled}
        aria-invalid={tooLong || undefined}
        aria-describedby={tooLong ? "partner-code-error" : undefined}
        className="w-full border border-(--input-edge) bg-(--card) px-3.5 py-[11px] text-[16px] text-black focus:border-(--focus) focus:outline-none"
      />
      {tooLong && (
        <p id="partner-code-error" className="text-[12px] text-destructive">
          Partner code must be 80 characters or fewer.
        </p>
      )}
      {value.trim() && (
        <p className="text-[12px] text-(--muted-strong)">
          For a new organization with a valid partner code: $100 off gateway
          egress per month for one year on pay-as-you-go, with no trial. A
          payment method is required. Gateway egress above the discount and
          other billable usage are charged normally. Unused discount does not
          roll over. This offer does not apply to existing organizations.
        </p>
      )}
    </div>
  );
}
