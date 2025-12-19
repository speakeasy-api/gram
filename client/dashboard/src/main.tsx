import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { Toaster } from "@/components/ui/sonner";
import App from "./App.tsx";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <style>
      @import
      url('https://fonts.googleapis.com/css2?family=Inter:ital,opsz,wght@0,14..32,100..900;1,14..32,100..900&family=Mona+Sans:ital,wght@0,200..900;1,200..900&family=Roboto:ital,wght@0,100;0,300;0,400;0,500;0,700;0,900;1,100;1,300;1,400;1,500;1,700;1,900&family=Space+Mono:ital,wght@0,400;0,700;1,400;1,700&display=swap');
    </style>
    <App />
    <Toaster />
  </StrictMode>,
);
