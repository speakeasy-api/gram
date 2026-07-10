// Injected via Vite' `define` config
declare const __GRAM_API_URL__: string | undefined;
declare const __GRAM_GIT_SHA__: string | undefined;

declare module "*.css?inline" {
  const content: string;
  export default content;
}
