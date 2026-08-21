// Product pillars, in brand order. Shared between the login panel and the
// demo-gate AuthLayout so the messaging can't fork between auth surfaces.
export const AUTH_PILLARS = ["Observe", "Secure", "Connect", "Distribute"];

// Primary CTA styling shared by the login and register panels: square,
// flat solid-ink button (mono uppercase) matching the dashboard's editorial
// design language.
export const AUTH_BUTTON_CLASSES =
  "auth-mono-text inline-flex h-10 items-center justify-center bg-[var(--cta)] text-[15px] leading-[1.6] tracking-[0.01em] text-white uppercase transition-colors hover:bg-[var(--cta-hover)]";
