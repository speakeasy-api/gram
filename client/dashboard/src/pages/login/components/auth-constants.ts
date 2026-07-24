// Product pillars, in brand order. Shared between the login panel and the
// demo-gate AuthLayout so the messaging can't fork between auth surfaces.
export const AUTH_PILLARS = ["Observe", "Secure", "Connect", "Distribute"];

// Primary CTA styling shared by the login and register panels: the
// marketing-site pill button (mono uppercase, inset bevel highlight).
export const AUTH_BUTTON_CLASSES =
  "auth-mono-text inline-flex h-10 items-center justify-center rounded-full bg-[var(--cta)] text-[15px] leading-[1.6] tracking-[0.01em] text-white uppercase shadow-[inset_0_2px_1px_0_#414141,inset_0_-2px_1px_0_rgba(0,0,0,0.05)] transition-colors hover:bg-[var(--cta-hover)]";
