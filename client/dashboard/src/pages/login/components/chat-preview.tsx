import { useState, useEffect, useCallback } from "react";
import { motion, AnimatePresence } from "motion/react";

type UploadStepState = "initial" | "uploaded";

// Step components
export function UploadStep({ onUploaded }: { onUploaded: () => void }) {
  const [state, setState] = useState<UploadStepState>("initial");

  useEffect(() => {
    let to2: NodeJS.Timeout;
    const to1 = setTimeout(() => {
      setState("uploaded");
      to2 = setTimeout(() => onUploaded(), 1500); // wait for drop feedback
    }, 1200);
    return () => {
      clearTimeout(to1);
      clearTimeout(to2);
    };
  }, []);

  return (
    <div className="flex flex-col w-full max-w-md">
      <div className="flex flex-col gap-4 mb-6">
        <motion.div
          className={`relative border-2 border-dashed rounded-md p-8 flex flex-col items-center transition-colors duration-300 ${
            state === "uploaded"
              ? "border-emerald-500 bg-emerald-500/10"
              : "border-zinc-700 bg-black"
          }`}
          whileHover={{ scale: 1.02, borderColor: "#ffffff" }}
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
              getPets(): GET /pets
            </div>
          </motion.div>
          <motion.div
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ delay: 0.4, duration: 0.4 }}
          >
            <div className="border border-zinc-700 rounded-md p-4 bg-zinc-900 text-white font-mono text-sm">
              createPet(): POST /pets
            </div>
          </motion.div>
        </div>
      )}
    </div>
  );
}

// function UserMessageStep() {
//   const [state, setState] = useState<UserMessageStepState>("typing");
//   const [text, setText] = useState("");
//   const [showCursor, setShowCursor] = useState(true);
//   const fullText = "Show me the latest deal in HubSpot";

//   useEffect(() => {
//     // Cursor blinking effect
//     const cursorInterval = setInterval(() => {
//       setShowCursor((prev) => !prev);
//     }, 500);

//     // Typing effect
//     if (state === "typing" && text.length < fullText.length) {
//       const timeout = setTimeout(() => {
//         setText(fullText.substring(0, text.length + 1));
//       }, 100);
//       return () => {
//         clearTimeout(timeout);
//         clearInterval(cursorInterval);
//       };
//     } else if (text.length === fullText.length && state === "typing") {
//       const timeout = setTimeout(() => setState("sent"), 500);
//       return () => {
//         clearTimeout(timeout);
//         clearInterval(cursorInterval);
//       };
//     }

//     return () => clearInterval(cursorInterval);
//   }, [text, state]);

//   return (
//     <div className="flex flex-col w-full max-w-md gap-6">
//       <h3 className="text-white text-lg">Playground</h3>
//       <p className="text-zinc-400 text-sm">
//         Experiment by calling your tools directly
//       </p>
//       {state === "sent" && (
//         <motion.div
//           initial={{ opacity: 0, y: 10 }}
//           animate={{ opacity: 1, y: 0 }}
//           className="flex items-start gap-3"
//         >
//           <div className="w-10 h-10 rounded-full overflow-hidden flex-shrink-0">
//             <div className="bg-zinc-800 text-white h-full w-full flex items-center justify-center">
//               🧑
//             </div>
//           </div>
//           <div className="bg-zinc-800 text-white rounded-3xl rounded-tl-sm px-5 py-3 text-base">
//             {fullText}
//           </div>
//         </motion.div>
//       )}

//       <div className="w-full mt-auto">
//         <div className="flex items-center bg-zinc-900 rounded-lg px-4 py-3">
//           <div className="flex-1 text-white text-base relative">
//             {state === "typing" ? (
//               <>
//                 {text}
//                 {showCursor && (
//                   <span className="inline-block w-2 h-4 bg-white ml-0.5 animate-pulse"></span>
//                 )}
//               </>
//             ) : (
//               <span className="text-zinc-400">Call your tools here...</span>
//             )}
//           </div>
//           <button
//             className={`ml-2 text-xl ${
//               state === "typing" ? "text-zinc-400" : "text-white"
//             }`}
//             disabled
//           >
//             <span role="img" aria-label="send">
//               →
//             </span>
//           </button>
//         </div>
//       </div>
//     </div>
//   );
// }

// function ToolCallStep() {
//   const [state, setState] = useState<ToolCallStepState>("initial");

//   useEffect(() => {
//     const timeout1 = setTimeout(() => setState("processing"), 1000);
//     const timeout2 = setTimeout(() => setState("completed"), 2500);

//     return () => {
//       clearTimeout(timeout1);
//       clearTimeout(timeout2);
//     };
//   }, []);

//   return (
//     <div className="flex flex-col w-full max-w-md gap-6">
//       <h3 className="text-white text-lg">Tool Execution</h3>
//       <p className="text-zinc-400 text-sm">
//         Watch your tool interact with the API
//       </p>
//       <div className="flex items-start gap-3">
//         <div className="w-10 h-10 rounded-full overflow-hidden flex-shrink-0">
//           <div className="bg-zinc-800 text-white h-full w-full flex items-center justify-center">
//             🧑
//           </div>
//         </div>
//         <div className="bg-zinc-800 text-white rounded-3xl rounded-tl-sm px-5 py-3 text-base">
//           Show me the latest deal in HubSpot
//         </div>
//       </div>

//       {(state === "processing" || state === "completed") && (
//         <div className="flex items-start gap-3 self-end">
//           {state === "processing" ? (
//             <motion.div
//               initial={{ opacity: 0 }}
//               animate={{ opacity: 1 }}
//               className="bg-zinc-800 p-4 rounded-md text-sm flex items-center gap-2"
//             >
//               {/* <TypingIndicator /> */}
//             </motion.div>
//           ) : (
//             <motion.div
//               initial={{ opacity: 0 }}
//               animate={{ opacity: 1 }}
//               className="bg-zinc-800 text-white p-3 rounded-md text-sm"
//             >
//               <div className="flex items-center gap-2 mb-2">
//                 <div className="h-2 w-2 bg-blue-400 rounded-full"></div>
//                 <span className="text-xs text-blue-400">Tool Call</span>
//               </div>
//               <div className="text-zinc-300 text-xs">hubspot_searchDeals</div>
//             </motion.div>
//           )}

//           <div className="bg-white w-10 h-10 rounded-sm flex items-center justify-center">
//             <span className="text-black text-lg">g</span>
//           </div>
//         </div>
//       )}

//       <div className="w-full mt-auto">
//         <div className="flex items-center bg-zinc-900 rounded-lg px-4 py-3">
//           <input
//             className="flex-1 bg-transparent text-white placeholder-zinc-400 outline-none text-base"
//             placeholder="What can we help you with?"
//             disabled
//           />
//           <button
//             className="ml-2 text-zinc-400 cursor-not-allowed text-xl"
//             disabled
//           >
//             <span role="img" aria-label="send">
//               →
//             </span>
//           </button>
//         </div>
//       </div>
//     </div>
//   );
// }

// function ResponseStep() {
//   const [state, setState] = useState<ResponseStepState>("initial");

//   useEffect(() => {
//     const timeout1 = setTimeout(() => setState("typing"), 500);
//     const timeout2 = setTimeout(() => setState("completed"), 2500);

//     return () => {
//       clearTimeout(timeout1);
//       clearTimeout(timeout2);
//     };
//   }, []);

//   return (
//     <div className="flex flex-col w-full max-w-md gap-6">
//       <h3 className="text-white text-lg">Result</h3>
//       <p className="text-zinc-400 text-sm">
//         See the output from your tool call
//       </p>
//       <div className="flex items-start gap-3">
//         <div className="w-10 h-10 rounded-full overflow-hidden flex-shrink-0">
//           <div className="bg-zinc-800 text-white h-full w-full flex items-center justify-center">
//             🧑
//           </div>
//         </div>
//         <div className="bg-zinc-800 text-white rounded-3xl rounded-tl-sm px-5 py-3 text-base">
//           Show me the latest deal in HubSpot
//         </div>
//       </div>

//       <div className="flex items-start gap-3 self-end">
//         {state === "typing" ? (
//           <motion.div
//             initial={{ opacity: 0 }}
//             animate={{ opacity: 1 }}
//             className="bg-white p-4 rounded-md text-sm"
//           >
//             {/* <TypingIndicator /> */}
//           </motion.div>
//         ) : state === "completed" ? (
//           <motion.div
//             initial={{ opacity: 0 }}
//             animate={{ opacity: 1 }}
//             className="bg-white text-black p-4 rounded-md text-base"
//           >
//             <p>The latest deal in HubSpot is:</p>
//             <ul className="mt-2 pl-5 list-disc">
//               <li>Deal Name: Intuit</li>
//               <li>Amount: $50,000</li>
//               <li>Created Date: September 26, 2023</li>
//             </ul>
//           </motion.div>
//         ) : null}

//         <div className="bg-white w-10 h-10 rounded-sm flex items-center justify-center">
//           <span className="text-black text-lg">g</span>
//         </div>
//       </div>

//       <div className="w-full mt-auto">
//         <div className="flex items-center bg-zinc-900 rounded-lg px-4 py-3">
//           <input
//             className="flex-1 bg-transparent text-white placeholder-zinc-400 outline-none text-base"
//             placeholder="What can we help you with?"
//             disabled
//           />
//           <button
//             className="ml-2 text-zinc-400 cursor-not-allowed text-xl"
//             disabled
//           >
//             <span role="img" aria-label="send">
//               →
//             </span>
//           </button>
//         </div>
//       </div>
//     </div>
//   );
// }

// function MultiUseStep() {
//   return (
//     <div className="flex flex-col w-full max-w-md gap-6 items-center text-center">
//       <h3 className="text-white text-xl">Use your tools anywhere</h3>
//       <div className="grid grid-cols-2 gap-4 mt-4">
//         <div className="flex flex-col items-center gap-2">
//           {/* Chat UI icon */}
//           <svg
//             className="h-8 w-8 text-blue-400"
//             viewBox="0 0 24 24"
//             fill="none"
//             stroke="currentColor"
//           >
//             <path d="M3 3h18v14H5l-2 2V3z" />
//           </svg>
//           <span className="text-zinc-300 text-sm">Chat UI</span>
//         </div>
//         <div className="flex flex-col items-center gap-2">
//           {/* MCP client icon */}
//           <svg
//             className="h-8 w-8 text-purple-400"
//             viewBox="0 0 24 24"
//             fill="none"
//             stroke="currentColor"
//           >
//             <circle cx="12" cy="12" r="10" />
//             <path d="M8 12l2 2l4-4" />
//           </svg>
//           <span className="text-zinc-300 text-sm">MCP Client</span>
//         </div>
//         <div className="flex flex-col items-center gap-2">
//           {/* Agent loop icon */}
//           <svg
//             className="h-8 w-8 text-green-400"
//             viewBox="0 0 24 24"
//             fill="none"
//             stroke="currentColor"
//           >
//             <path d="M2 12a10 10 0 0 1 18-6" />
//             <path d="M22 12a10 10 0 0 1-18 6" />
//           </svg>
//           <span className="text-zinc-300 text-sm">Agent Loop</span>
//         </div>
//         <div className="flex flex-col items-center gap-2">
//           {/* Slack integration icon */}
//           <svg
//             className="h-8 w-8 text-pink-400"
//             viewBox="0 0 24 24"
//             fill="none"
//             stroke="currentColor"
//           >
//             <path d="M14.5 3h-5v5" />
//             <path d="M9.5 21h5v-5" />
//             <path d="M21 9.5v5h-5" />
//             <path d="M3 14.5v-5h5" />
//           </svg>
//           <span className="text-zinc-300 text-sm">Slack Integration</span>
//         </div>
//       </div>
//     </div>
//   );
// }

export function JourneyDemo() {
  const [step, setStep] = useState(0);
  const handleUploadComplete = useCallback(() => {
    setStep(1);
  }, []);
  // Advance automatically for demo; in real use, you'd trigger next manually.
  useEffect(() => {
    if (step === 0) return;
  }, [step]);

  useEffect(() => {
    if (step === 1) {
      const loop = setTimeout(() => setStep(0), 4000);
      return () => clearTimeout(loop);
    }
    return;
  }, [step]);

  const steps = [
    <UploadStep onUploaded={handleUploadComplete} key="upload" />,
    <GenerateStep key="generate" />,
    // add other steps here when ready...
  ];

  return (
    <div className="flex flex-col justify-center items-center w-full md:w-1/2 min-h-screen bg-black relative border-gradient-primary border-8 border-t-0 border-x-0 p-8">
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
