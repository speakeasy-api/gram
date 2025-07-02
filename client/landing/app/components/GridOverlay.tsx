export function GridOverlay({ inset = "2rem" }: { inset?: string }) {
  return (
    <div className="pointer-events-none absolute inset-0 z-10 w-full h-full">
      {/* Horizontal lines */}
      <div
        className="absolute"
        style={{ left: 0, right: 0, top: inset, height: 1 }}
      >
        <div
          className="w-full h-px"
          style={{
            background:
              "linear-gradient(to right, var(--color-neutral-300) 0%, transparent 100%)",
          }}
        />
      </div>
      <div
        className="absolute"
        style={{ left: 0, right: 0, bottom: inset, height: 1 }}
      >
        <div
          className="w-full h-px"
          style={{
            background:
              "linear-gradient(to left, var(--color-neutral-300) 0%, transparent 100%)",
          }}
        />
      </div>

      {/* Vertical lines */}
      <div
        className="absolute"
        style={{ top: 0, bottom: 0, left: inset, width: 1 }}
      >
        <div
          className="h-full w-px"
          style={{
            background:
              "linear-gradient(to bottom, var(--color-neutral-300) 0%, transparent 100%)",
          }}
        />
      </div>
      <div
        className="absolute"
        style={{ top: 0, bottom: 0, right: inset, width: 1 }}
      >
        <div
          className="h-full w-px"
          style={{
            background:
              "linear-gradient(to top, var(--color-neutral-300) 0%, transparent 100%)",
          }}
        />
      </div>

      {/* Corner squares */}
      <div
        className="absolute"
        style={{
          width: 10,
          height: 10,
          left: inset,
          top: inset,
          background: "var(--color-background)",
          border: "2px solid var(--color-neutral-300)",
          borderRadius: 2,
          transform: "translate(-50%, -50%)",
        }}
      />
      <div
        className="absolute"
        style={{
          width: 10,
          height: 10,
          right: inset,
          top: inset,
          background: "var(--color-background)",
          border: "2px solid var(--color-neutral-300)",
          borderRadius: 2,
          transform: "translate(50%, -50%)",
        }}
      />
      <div
        className="absolute"
        style={{
          width: 10,
          height: 10,
          left: inset,
          bottom: inset,
          background: "var(--color-background)",
          border: "2px solid var(--color-neutral-300)",
          borderRadius: 2,
          transform: "translate(-50%, 50%)",
        }}
      />
      <div
        className="absolute"
        style={{
          width: 10,
          height: 10,
          right: inset,
          bottom: inset,
          background: "var(--color-background)",
          border: "2px solid var(--color-neutral-300)",
          borderRadius: 2,
          transform: "translate(50%, 50%)",
        }}
      />
    </div>
  );
}
