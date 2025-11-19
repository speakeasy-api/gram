import { useState, useEffect, useCallback } from "react";
import { motion, AnimatePresence } from "motion/react";
import { GridOverlay } from "./grid-overlay";

type UploadStepState = "initial" | "uploaded";

// Step components
export function UploadStep({ onUploaded }: { onUploaded: () => void }) {
  const [state, setState] = useState<UploadStepState>("initial");

  useEffect(() => {
    const to1 = setTimeout(() => {
      setState("uploaded");
      const to2 = setTimeout(() => onUploaded(), 1500); // wait for drop feedback
      return () => clearTimeout(to2);
    }, 1200);

    return () => clearTimeout(to1);
  }, [onUploaded]);

  return (
    <div className="flex flex-col w-full max-w-md">
      <div className="flex flex-col gap-4 mb-6">
        <motion.div
          className={`relative border-2 border-dashed rounded-md p-8 flex flex-col items-center transition-colors duration-300 ${
            state === "uploaded"
              ? "border-emerald-500 bg-emerald-500/10"
              : "border-zinc-700 bg-black"
          }`}
          transition={{ type: "spring", stiffness: 150, damping: 20 }}
        >
          {/* Drop feedback */}
          {state === "uploaded" && (
            <motion.div
              className="absolute inset-0 rounded-md bg-emerald-500/20"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              transition={{ duration: 0.2 }}
            />
          )}
          {/* Drag animation */}
          {state === "initial" && (
            <motion.div
              className="absolute"
              initial={{ x: -200, y: 200, rotate: -15 }}
              animate={{ x: 0, y: 0, rotate: 0 }}
              transition={{ duration: 1.2, ease: "easeInOut" }}
            >
              <div className="w-24 h-28 bg-white rounded-lg shadow-lg border border-zinc-200 flex flex-col overflow-hidden">
                <div className="h-3 bg-zinc-100 flex-shrink-0"></div>
                <div className="flex-1 p-2">
                  {[...Array(5)].map((_, i) => (
                    <div key={i} className="h-1 bg-zinc-300 my-1 rounded"></div>
                  ))}
                </div>
              </div>
              <div className="absolute -bottom-3 left-1/2 transform -translate-x-1/2 bg-zinc-900 text-white text-xs px-2 py-0.5 rounded">
                petstore.yaml
              </div>
            </motion.div>
          )}
          {/* Icon */}
          <div className="text-zinc-400 mb-1">
            {state === "initial" ? (
              <svg
                xmlns="http://www.w3.org/2000/svg"
                width="24"
                height="24"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
              >
                <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
                <polyline points="17 8 12 3 7 8" />
                <line x1="12" y1="3" x2="12" y2="15" />
              </svg>
            ) : state === "uploaded" ? (
              <motion.div
                initial={{ scale: 0 }}
                animate={{ scale: 1 }}
                transition={{ type: "spring", bounce: 0.5 }}
              >
                <svg
                  xmlns="http://www.w3.org/2000/svg"
                  width="24"
                  height="24"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="#10b981"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                >
                  <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14" />
                  <polyline points="22 4 12 14.01 9 11.01" />
                </svg>
              </motion.div>
            ) : null}
          </div>

          <div className="text-zinc-400 text-center">
            {state === "initial" && (
              <>
                <p>Drag and drop your OpenAPI file here</p>
                <p className="text-sm">yaml, yml, json</p>
              </>
            )}
            {state === "uploaded" && (
              <motion.p
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                className="text-emerald-400"
              >
                API spec uploaded successfully!
              </motion.p>
            )}
          </div>
        </motion.div>
      </div>
    </div>
  );
}

export function GenerateStep() {
  const [state, setState] = useState<"initial" | "generated">("initial");

  useEffect(() => {
    const timer = setTimeout(() => setState("generated"), 1500);
    return () => clearTimeout(timer);
  }, []);

  return (
    <div className="flex flex-col w-full max-w-md gap-6">
      {state === "initial" && (
        <motion.div
          className="flex items-center gap-3 border border-zinc-700 rounded-md p-4"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ duration: 0.4 }}
        >
          <span className="text-zinc-400">Generating tools...</span>
        </motion.div>
      )}

      {state === "generated" && (
        <div className="flex flex-col gap-3">
          <motion.div
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ delay: 0.2, duration: 0.4 }}
          >
            <div className="border border-zinc-700 rounded-md p-4 bg-zinc-900 text-white font-mono text-sm">
              getPets(): <span className="text-blue-400">GET</span> /pets
            </div>
          </motion.div>
          <motion.div
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ delay: 0.4, duration: 0.4 }}
          >
            <div className="border border-zinc-700 rounded-md p-4 bg-zinc-900 text-white font-mono text-sm">
              createPet(): <span className="text-emerald-400">POST</span> /pets
            </div>
          </motion.div>
        </div>
      )}
    </div>
  );
}

export function JourneyDemo() {
  const [step, setStep] = useState(0);

  const handleUploadComplete = useCallback(() => {
    setStep(1);
  }, []);

  useEffect(() => {
    if (step === 1) {
      const loop = setTimeout(() => setStep(0), 4000);
      return () => clearTimeout(loop);
    }
    return () => {};
  }, [step]);

  const steps = [
    <UploadStep onUploaded={handleUploadComplete} key="upload" />,
    <GenerateStep key="generate" />,
  ];

  return (
    <div className="flex flex-col justify-center items-center w-full md:w-1/2 min-h-screen bg-black relative border-gradient-primary border-8 border-t-0 border-x-0 p-8">
      <GridOverlay />
      <div className="flex-1 flex items-center justify-center w-full relative overflow-hidden">
        <AnimatePresence mode="wait">
          <motion.div
            key={step}
            initial={{ x: 300, opacity: 0 }}
            animate={{ x: 0, opacity: 1 }}
            exit={{ x: -300, opacity: 0 }}
            transition={{ type: "spring", stiffness: 200, damping: 30 }}
            className="absolute inset-0 flex items-center justify-center"
          >
            {steps[step]}
          </motion.div>
        </AnimatePresence>
      </div>
    </div>
  );
}
