import { EmptyState } from "@/components/page-layout";
import { Button } from "@speakeasy-api/moonshine";
import { CheckCircle } from "lucide-react";

export function ApisEmptyState({ onNewUpload }: { onNewUpload: () => void }) {
  const cta = (
    <Button size="sm" onClick={onNewUpload}>
      UPLOAD OPENAPI
    </Button>
  );

  return (
    <EmptyState
      heading="No OpenAPI documents yet"
      description="Gram generates MCP-ready tools from your OpenAPI documents. Upload an OpenAPI document to get started."
      nonEmptyProjectCTA={cta}
      graphic={<OpenapiGraphic />}
    />
  );
}

function OpenapiGraphic() {
  return (
    <div className="w-full max-w-sm scale-110">
      <div className="relative h-[160px]">
        {/* Background: OpenAPI Spec - more visible */}
        <div
          className="absolute left-[10%] top-1/2 -translate-y-1/2 w-[55%] bg-gradient-to-br from-neutral-100 to-neutral-50 rounded-lg overflow-hidden border border-neutral-200"
          style={{
            zIndex: 1,
            opacity: 0.8,
          }}
        >
          <div className="flex items-center gap-1.5 p-1.5 bg-white border-b border-neutral-200">
            <svg width="12" height="12" viewBox="0 0 16 16" fill="none">
              <rect
                x="2"
                y="3"
                width="12"
                height="10"
                rx="1"
                className="stroke-neutral-400"
                strokeWidth="1.5"
              />
              <path
                d="M5 6.5H11M5 9.5H9"
                className="stroke-neutral-400"
                strokeWidth="1.5"
                strokeLinecap="round"
              />
            </svg>
            <span className="text-[8px] font-medium text-neutral-700">
              PETSTORE.YAML
            </span>
          </div>
          <div className="p-2 font-mono text-[7px] leading-[1.2] space-y-0.5">
            <div className="flex">
              <span className="text-neutral-400 mr-1 select-none">1</span>
              <span className="text-brand-green-600">openapi</span>
              <span className="text-neutral-600">: </span>
              <span className="text-brand-blue-600">3.0.0</span>
            </div>
            <div className="flex">
              <span className="text-neutral-400 mr-1 select-none">2</span>
              <span className="text-brand-green-600">paths</span>
            </div>
            <div className="flex">
              <span className="text-neutral-400 mr-1 select-none">3</span>
              <span className="ml-1 text-brand-green-600">/pet</span>
            </div>
          </div>
        </div>

        {/* Foreground: Tools card */}
        <div className="absolute right-[10%] top-1/2 -translate-y-1/2 w-[55%] z-10">
          <div className="w-full bg-white rounded-lg overflow-hidden border border-neutral-200">
            <div className="flex items-center justify-between p-1.5 border-b border-neutral-200">
              <h4 className="text-[8px] font-medium text-neutral-900">
                Auto-generated Tools
              </h4>
              <CheckCircle className="w-3 h-3 text-success-600" />
            </div>
            <div className="p-2 overflow-hidden">
              <div className="space-y-1 overflow-hidden">
                {[
                  {
                    name: "findPetById",
                    desc: "GET /pet/{id}",
                    color: "blue",
                  },
                  {
                    name: "deletePet",
                    desc: "DELETE /pet/{id}",
                    color: "red",
                  },
                  { name: "addPet", desc: "POST /pet", color: "green" },
                ].map((tool) => (
                  <div
                    key={tool.name}
                    className="flex items-center gap-2 p-1 rounded-md"
                  >
                    <div
                      className={`w-1 h-1 rounded-full ${
                        tool.color === "blue"
                          ? "bg-brand-blue-500"
                          : tool.color === "green"
                          ? "bg-brand-green-500"
                          : tool.color === "red"
                          ? "bg-brand-red-500"
                          : ""
                      }`}
                    />
                    <div className="flex-1">
                      <div className="font-mono text-[8px] text-neutral-900">
                        {tool.name}
                      </div>
                      <div className="text-[7px] text-neutral-500">
                        {tool.desc}
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
