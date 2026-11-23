// A little fanned stack of client badges — purely decorative, gestures at
// "this is what publishing unlocks" without claiming these are the only
// supported clients.
export function ClientIconFan(): React.JSX.Element {
  return (
    <div className="relative hidden h-36 shrink-0 sm:block" aria-hidden="true">
      <div className="absolute inset-6 rounded-full bg-black/15 blur-2xl" />
      <img
        src="/icons/decorative/plugin-clients.webp"
        alt=""
        className="relative h-full w-full mr-10 object-contain drop-shadow-xl"
      />
    </div>
  );
}
