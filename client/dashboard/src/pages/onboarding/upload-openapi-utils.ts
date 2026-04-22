import { useProject, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { slugify } from "@/lib/constants";
import { getServerURL } from "@/lib/utils";
import {
  Deployment,
  GetDeploymentResult,
  UploadOpenAPIv3Result,
} from "@gram/client/models/components";
import { assetsServeOpenAPIv3 } from "@gram/client/funcs/assetsServeOpenAPIv3";
import {
  useLatestDeployment,
  useListToolsets,
} from "@gram/client/react-query/index.js";
import { useState } from "react";
import { toast } from "sonner";
import { useParams } from "react-router";

export function useUploadOpenAPISteps(checkDocumentSlugUnique = true) {
  const project = useProject();
  const session = useSession();
  const client = useSdkClient();
  const telemetry = useTelemetry();

  const { data: latestDeployment } = useLatestDeployment();

  const [file, setFile] = useState<File>();
  const [asset, setAsset] = useState<UploadOpenAPIv3Result>();
  const [isUploading, setIsUploading] = useState(false);
  const [creatingDeployment, setCreatingDeployment] = useState(false);
  const [apiName, setApiName] = useState<string | undefined>();
  const [deployment, setDeployment] = useState<Deployment>();

  // If an existing document slug was NOT provided, then we need to make sure the provided slug
  // isn't accidentally overwriting an existing document slug.
  let apiNameError: string | undefined;

  if (apiName) {
    if (apiName.length < 3) {
      apiNameError = "API name must be at least 3 characters long";
    }

    if (
      checkDocumentSlugUnique &&
      latestDeployment?.deployment?.openapiv3Assets
        .map((a) => a.slug)
        .includes(apiName)
    ) {
      apiNameError = "API name must be unique";
    }
  } else {
    apiNameError = "API name is required";
  }

  const getContentType = (file: File) => {
    if (file.type) return file.type;
    const ext = file.name.split(".").pop()?.toLowerCase();
    switch (ext) {
      case "json":
        return "application/json";
      case "yaml":
      case "yml":
        return "application/yaml";
      default:
        return "application/octet-stream";
    }
  };

  const handleSpecUpload = async (file: File) => {
    try {
      setIsUploading(true);

      telemetry.capture("onboarding_event", {
        action: "spec_uploaded",
      });

      // Need to use fetch directly because the SDK doesn't support file uploads
      const response = await fetch(
        `${getServerURL()}/rpc/assets.uploadOpenAPIv3`,
        {
          method: "POST",
          headers: {
            "content-type": getContentType(file),
            "content-length": file.size.toString(),
            "gram-session": session.session,
            "gram-project": project.slug,
          },
          body: file,
        },
      );

      if (!response.ok) {
        throw new Error(`Upload failed`);
      }

      const result: UploadOpenAPIv3Result = await response.json();

      setAsset(result);
      setFile(file);
      if (!apiName) {
        setApiName(slugify(file?.name.split(".")[0] ?? "My API"));
      }
    } catch (_error) {
      toast.error("Failed to upload OpenAPI spec");
    } finally {
      setIsUploading(false);
    }
  };

  const handleUrlUpload = async (result: UploadOpenAPIv3Result) => {
    setIsUploading(true);
    try {
      const response = await assetsServeOpenAPIv3(client, {
        id: result.asset.id,
        projectId: project.id,
      });
      if (!response.ok) {
        toast.error(
          `Failed to fetch OpenAPI content: ${response.error.message}`,
        );
        return;
      }

      // Convert ReadableStream to Blob
      const blob = await new Response(response.value.result).blob();

      setAsset(result);
      setFile(
        new File([blob], "My API", {
          type: result.asset.contentType,
        }),
      );

      telemetry.capture("onboarding_event", {
        action: "spec_uploaded",
        source: "url",
      });
      if (!apiName) {
        setApiName("My API");
      }
    } catch (_error) {
      toast.error("Failed to load OpenAPI spec from URL");
    } finally {
      setIsUploading(false);
    }
  };

  const createDeployment = async (documentSlug?: string, forceNew = false) => {
    if (!asset || !apiName) {
      throw new Error("Asset or file not found");
    }

    setCreatingDeployment(true);

    try {
      const shouldCreateNew =
        !latestDeployment ||
        (forceNew && latestDeployment.deployment?.openapiv3ToolCount === 0);

      let deployment: Deployment | undefined;
      if (shouldCreateNew) {
        const createResult = await client.deployments.create({
          idempotencyKey: crypto.randomUUID(),
          createDeploymentRequestBody: {
            nonBlocking: true,
            openapiv3Assets: [
              {
                assetId: asset.asset.id,
                name: documentSlug ?? apiName,
                slug: documentSlug ?? slugify(apiName),
              },
            ],
          },
        });

        deployment = createResult.deployment;
      } else {
        const createResult = await client.deployments.evolveDeployment({
          nonBlocking: true,
          upsertOpenapiv3Assets: [
            {
              assetId: asset.asset.id,
              name: documentSlug ?? apiName,
              slug: documentSlug ?? slugify(apiName),
            },
          ],
        });

        deployment = createResult.deployment;
      }

      if (!deployment) {
        throw new Error("Deployment not found");
      }

      // Wait for deployment to finish
      const maxAttempts = 600; // 5 minutes at 500ms intervals
      let attempts = 0;
      while (
        deployment.status !== "completed" &&
        deployment.status !== "failed"
      ) {
        if (++attempts >= maxAttempts) {
          throw new Error("Deployment timed out waiting for completion");
        }
        await new Promise((resolve) => setTimeout(resolve, 500));
        deployment = await client.deployments.getById({
          id: deployment.id,
        });
      }

      setDeployment(deployment);

      if (deployment.status === "failed") {
        telemetry.capture("onboarding_event", {
          action: "deployment_failed",
        });
      } else {
        telemetry.capture("onboarding_event", {
          action: "deployment_created",
          num_tools: deployment?.openapiv3ToolCount,
        });
      }

      if (deployment?.openapiv3ToolCount === 0) {
        telemetry.capture("onboarding_event", {
          action: "no_tools_found",
          error: "no_tools_found",
        });
      }

      return deployment;
    } finally {
      setCreatingDeployment(false);
    }
  };

  const undoSpecUpload = () => {
    setFile(undefined);
    setAsset(undefined);
    setApiName(undefined);
  };

  return {
    apiNameError,
    handleSpecUpload,
    handleUrlUpload,
    undoSpecUpload,
    apiName,
    setApiName,
    createDeployment,
    file,
    asset,
    isUploading,
    createdDeployment: deployment,
    creatingDeployment,
  };
}

export function useIsProjectEmpty() {
  const { projectSlug } = useParams();

  const { data: deployment, isLoading: isDeploymentLoading } =
    useLatestDeployment({ gramProject: projectSlug });
  const { data: toolsets, isLoading: isToolsetsLoading } = useListToolsets({
    gramProject: projectSlug,
  });

  return {
    isLoading: isDeploymentLoading || isToolsetsLoading,
    isEmpty:
      isDeploymentEmpty(deployment?.deployment) &&
      toolsets?.toolsets.length === 0,
  };
}

function isDeploymentEmpty(deployment: GetDeploymentResult | undefined) {
  return (
    !deployment ||
    (deployment?.openapiv3Assets.length === 0 &&
      (deployment.functionsAssets?.length ?? 0) === 0 &&
      deployment?.packages.length === 0)
  );
}
