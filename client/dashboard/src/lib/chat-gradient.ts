/**
 * The Speakeasy brand rainbow used behind the chat landing header and the
 * suggestion-launch overlay. Each blob fades to its own zero-alpha so the
 * overlaps read as soft powder rather than muddy grey. Shared so the launch
 * overlay's underlay is literally the same gradient the landing page shows.
 */
export const CHAT_LANDING_GRADIENT = [
  "radial-gradient(38% 48% at 30% 42%, #C83228 0%, rgba(200,50,40,0) 70%)",
  "radial-gradient(36% 46% at 48% 28%, #FB873F 0%, rgba(251,135,63,0) 70%)",
  "radial-gradient(42% 52% at 64% 40%, #D2DC91 0%, rgba(210,220,145,0) 72%)",
  "radial-gradient(44% 54% at 70% 60%, #5A8250 0%, rgba(90,130,80,0) 72%)",
  "radial-gradient(42% 52% at 42% 62%, #2873D7 0%, rgba(40,115,215,0) 72%)",
  "radial-gradient(36% 46% at 26% 54%, #9BC3FF 0%, rgba(155,195,255,0) 72%)",
].join(",");

/** Blob geometry shared by the landing backdrop and the launch overlay. */
export const CHAT_LANDING_GRADIENT_CLASS =
  "h-[560px] w-[920px] max-w-[140vw] opacity-60 blur-[72px] dark:opacity-40";
