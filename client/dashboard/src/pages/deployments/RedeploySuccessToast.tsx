const RedeploySuccessToast = ({
  href,
}: {
  href: string | undefined;
}): JSX.Element => {
  if (!href) return <p>Successfully redeployed!</p>;

  return (
    <p>
      Successfully redeployed!{" "}
      <a href={href} className="underline">
        View deployment
      </a>
      .
    </p>
  );
};

export default RedeploySuccessToast;
