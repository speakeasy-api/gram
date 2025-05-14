export function GridOverlay() {
  // Inset value matches p-8 (2rem)
  const inset = "4rem";
  return (
    <div className="pointer-events-none absolute inset-0 z-20 w-full h-full">
      {/* Horizontal lines */}
      <div
        className="absolute"
        style={{ left: 0, right: 0, top: inset, height: 2 }}
      >
        <div
          className="w-full h-px"
          style={{
            background: "linear-gradient(to right, #fff 0%, #fff0 100%)",
            opacity: 0.2,
          }}
        />
      </div>
      <div
        className="absolute"
        style={{ left: 0, right: 0, bottom: inset, height: 2 }}
      >
        <div
          className="w-full h-px"
          style={{
            background: "linear-gradient(to right, #fff 0%, #fff0 100%)",
            opacity: 0.2,
            transform: "rotate(180deg)",
          }}
        />
      </div>
      {/* Vertical lines */}
      <div
        className="absolute"
        style={{ top: 0, bottom: 0, left: inset, width: 2 }}
      >
        <div
          className="h-full w-px"
          style={{
            background: "linear-gradient(to bottom, #fff 0%, #fff0 100%)",
            opacity: 0.2,
          }}
        />
      </div>
      <div
        className="absolute"
        style={{ top: 0, bottom: 0, right: inset, width: 2 }}
      >
        <div
          className="h-full w-px"
          style={{
            background: "linear-gradient(to bottom, #fff 0%, #fff0 100%)",
            opacity: 0.2,
            transform: "rotate(180deg)",
          }}
        />
      </div>
      {/* Intersection squares */}
      <div
        className="absolute"
        style={{
          width: 10,
          height: 10,
          left: "calc(4rem + 0.5px)",
          top: "calc(4rem + 0.5px)",
          background: "#000",
          border: "2px solid rgba(255,255,255,0.2)",
          boxSizing: "border-box",
          borderRadius: 2,
          transform: "translate(-50%, -50%)",
        }}
      />
      <div
        className="absolute"
        style={{
          width: 10,
          height: 10,
          left: "calc(100% - 1.5px - 4rem)",
          top: "calc(100% - 1.5px - 4rem)",
          background: "#000",
          border: "2px solid rgba(255,255,255,0.2)",
          boxSizing: "border-box",
          borderRadius: 2,
          transform: "translate(-50%, -50%)",
        }}
      />
    </div>
  );
}
