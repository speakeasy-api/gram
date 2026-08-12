import { cn, getServerURL } from "@/lib/utils";

export const AssetImage = ({
  assetId,
  className,
}: {
  assetId: string;
  className?: string;
}): JSX.Element => {
  return (
    <img
      src={`${getServerURL()}/rpc/assets.serveImage?id=${assetId}`}
      alt={"Uploaded image"}
      className={cn("h-[200px] w-[200px]", className)}
    />
  );
};
