// TypeScript 7 checks side-effect imports (TS2882), and neither Next's global
// types nor the generated (gitignored) next-env.d.ts declare plain "*.css"
// imports, so declare them here for `tsc --noEmit`.
declare module "*.css";
