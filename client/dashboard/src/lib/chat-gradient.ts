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

/**
 * Faint mesh laid over the project home assistant card. Three mid-tone brand
 * hues, widely spaced and soft-light blended, so it tints whichever surface it
 * sits on — barely-there warmth on the left, cool on the right — rather than
 * painting a colour of its own.
 */
export const HOME_ASSISTANT_GRADIENT = [
  "radial-gradient(70% 120% at 6% 10%, #C2603A 0%, rgba(194,96,58,0) 70%)",
  "radial-gradient(80% 130% at 96% 40%, #2E7D93 0%, rgba(46,125,147,0) 72%)",
  "radial-gradient(60% 110% at 55% 105%, #4E7A4A 0%, rgba(78,122,74,0) 72%)",
].join(",");

/**
 * Film grain laid over the mesh — an inline SVG turbulence tile, so no network
 * request and no image asset. Blended and heavily faded; purely texture.
 */
export const GRAIN_TEXTURE_URL =
  "url(\"data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='140' height='140'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='3'/%3E%3C/filter%3E%3Crect width='140' height='140' filter='url(%23n)'/%3E%3C/svg%3E\")";

/** Blob geometry shared by the landing backdrop and the launch overlay. */
export const CHAT_LANDING_GRADIENT_CLASS =
  "h-[560px] w-[920px] max-w-[140vw] opacity-60 blur-[72px] dark:opacity-40";
