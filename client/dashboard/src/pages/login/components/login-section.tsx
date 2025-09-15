"use client";

import { Button } from "@speakeasy-api/moonshine";
import { getServerURL } from "@/lib/utils";
import { useSearchParams } from "react-router";
import { useState } from "react";
import {
  buildRegisterMutation,
  RegisterMutationVariables,
  useGramContext,
} from "@gram/client/react-query";
import { authInfo } from "@gram/client/funcs/authInfo";
import { useTelemetry } from "@/contexts/Telemetry";
import { useMutation } from "@tanstack/react-query";
import { GramLogo } from "@/components/gram-logo/index";

const unexpected = "Server error. Please try again later or contact support.";
const authErrorMessages: Record<string, string> = {
  lookup_error: "Failed to look up account details.",
  init_error: "Failed to initialize account.",
  local_dev_stubbed: "Authentication is stubbed during local development.",
  unexpected,
};

function getAuthErrorMessage(errorCode?: string | null): string {
  if (!errorCode) {
    return unexpected;
  }
  return authErrorMessages[errorCode] || unexpected;
}

const Logo = () => {
  return (
    <svg
      width="161"
      height="25"
      viewBox="0 0 161 25"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
    >
      <path
        d="M157.67 5.30535L153.672 16.2464L149.543 5.30535H146.837L152.307 19.3631L151.629 20.9542C151.437 21.4261 151.27 21.7604 151.137 21.9439L151.126 21.9603C151.018 22.1471 150.875 22.2749 150.69 22.3503C150.495 22.4322 150.188 22.4732 149.779 22.4732H147.642V24.8295H150.455C151.111 24.8295 151.645 24.723 152.047 24.5133C152.45 24.3019 152.791 23.9725 153.056 23.5334C153.312 23.1122 153.608 22.4765 153.934 21.6424L160.376 5.30371H157.669L157.67 5.30535Z"
        fill="currentColor"
      />
      <path
        d="M144.362 11.6142C143.58 11.3602 142.569 11.157 141.357 11.0128C140.5 10.9063 139.851 10.8064 139.425 10.7179C139.041 10.6343 138.722 10.4754 138.474 10.2443C138.237 10.0231 138.121 9.70848 138.121 9.28244C138.121 8.70565 138.379 8.25503 138.915 7.90272C139.461 7.52748 140.195 7.3374 141.093 7.3374C141.991 7.3374 142.71 7.56189 143.292 8.00595C143.874 8.45166 144.198 8.99896 144.283 9.68063L144.301 9.82482H146.84L146.826 9.64785C146.715 8.19931 146.12 7.06702 145.058 6.2854C144.024 5.51033 142.664 5.11707 141.013 5.11707C140.003 5.11707 139.079 5.30387 138.27 5.67092C137.454 6.02322 136.795 6.53283 136.31 7.18664C135.841 7.84373 135.604 8.60241 135.604 9.44466C135.604 10.3836 135.854 11.139 136.351 11.6929C136.837 12.2172 137.48 12.6039 138.261 12.8464C139.023 13.0824 140.016 13.2758 141.209 13.42C142.047 13.5265 142.696 13.6346 143.14 13.7411C143.575 13.8411 143.931 14.0279 144.197 14.2917C144.462 14.5555 144.582 14.9259 144.582 15.4191C144.582 15.9959 144.305 16.4547 143.738 16.8217C143.172 17.1806 142.41 17.3641 141.473 17.3641C140.415 17.3641 139.538 17.1118 138.868 16.6136C138.217 16.1171 137.885 15.4945 137.85 14.7096L137.844 14.5522H135.304V14.7177C135.325 16.2417 135.9 17.4493 137.021 18.308C138.143 19.1551 139.641 19.5845 141.473 19.5845C142.499 19.5845 143.448 19.4075 144.293 19.0585C145.145 18.7062 145.832 18.1982 146.335 17.5476C146.841 16.8906 147.099 16.1204 147.099 15.2569C147.099 14.2655 146.84 13.4658 146.33 12.8792C145.843 12.2991 145.181 11.8731 144.364 11.6126L144.362 11.6142Z"
        fill="currentColor"
      />
      <path
        d="M133.684 16.87C133.559 16.7356 133.497 16.5194 133.497 16.231V10.2828C133.497 8.63758 132.987 7.35782 131.983 6.48444C131.001 5.5963 129.591 5.14404 127.791 5.14404C126.105 5.14404 124.709 5.53731 123.639 6.31074C122.579 7.09236 121.956 8.19516 121.789 9.58635L121.768 9.76987H124.303L124.329 9.63878C124.465 8.9735 124.814 8.45897 125.397 8.06407C125.997 7.65277 126.767 7.44467 127.685 7.44467C128.722 7.44467 129.532 7.68554 130.095 8.16402C130.674 8.64086 130.955 9.28975 130.955 10.1468V10.9284H127.064C125.232 10.9284 123.806 11.3135 122.839 12.0623L122.825 12.0705C121.85 12.839 121.356 13.9549 121.356 15.3903C121.356 16.6816 121.841 17.7155 122.8 18.4677C123.762 19.2083 125.042 19.5836 126.603 19.5836C128.511 19.5836 130.021 18.9052 131.1 17.5648C131.18 18.099 131.383 18.5267 131.708 18.8347C132.116 19.2231 132.791 19.4213 133.713 19.4213H135.093V17.065H134.254C133.993 17.065 133.807 17.0011 133.685 16.8684L133.684 16.87ZM130.952 13.1241V13.6354C130.952 14.7546 130.579 15.6689 129.843 16.3539C129.102 17.0257 128.067 17.3649 126.764 17.3649C125.886 17.3649 125.183 17.1666 124.673 16.7766C124.17 16.3915 123.926 15.895 123.926 15.2576C123.926 14.5399 124.157 14.0188 124.63 13.6649C125.112 13.306 125.858 13.1225 126.844 13.1225H130.952V13.1241Z"
        fill="currentColor"
      />
      <path
        d="M117.065 5.94697C116.072 5.41442 114.919 5.14404 113.636 5.14404C112.353 5.14404 111.158 5.4521 110.145 6.05839C109.133 6.6483 108.328 7.4971 107.758 8.58351C107.207 9.66664 106.929 10.9382 106.929 12.3638C106.929 13.7894 107.217 15.043 107.787 16.1457C108.376 17.2305 109.206 18.0892 110.257 18.6971C111.304 19.2853 112.541 19.5836 113.934 19.5836C115.533 19.5836 116.905 19.1248 118.014 18.2202C119.12 17.2977 119.821 16.1015 120.098 14.6644L120.136 14.4694H117.568L117.536 14.5907C117.313 15.4313 116.86 16.1015 116.193 16.5816C115.54 17.0453 114.725 17.2813 113.773 17.2813C112.546 17.2813 111.538 16.888 110.781 16.1146C110.022 15.3379 109.619 14.2843 109.583 12.9832V12.9586H120.277L120.29 12.8079C120.326 12.3654 120.344 12.0525 120.344 11.8444C120.308 10.4909 120 9.29139 119.431 8.28036C118.86 7.26606 118.065 6.48116 117.069 5.94533L117.065 5.94697ZM117.698 10.7956H109.737C109.87 9.82558 110.299 9.02266 111.012 8.40654C111.771 7.75109 112.662 7.41845 113.663 7.41845C114.789 7.41845 115.726 7.73306 116.449 8.35246C117.147 8.93745 117.567 9.7584 117.696 10.7956H117.698Z"
        fill="currentColor"
      />
      <path
        d="M107.327 5.30712H104.229L97.4595 12.4679V0.170044H94.9147V17.0675H93.0467C92.7861 17.0675 92.5993 17.0036 92.478 16.8709C92.3535 16.7365 92.2912 16.5202 92.2912 16.2318V10.2836C92.2912 8.63844 91.7816 7.35868 90.7772 6.48529C89.7956 5.59716 88.3848 5.1449 86.5856 5.1449C84.8994 5.1449 83.5033 5.53817 82.4333 6.3116C81.3731 7.09322 80.7504 8.19601 80.5833 9.5872L80.562 9.77073H83.0969L83.1232 9.63964C83.2592 8.97436 83.6082 8.45983 84.1915 8.06492C84.7913 7.65363 85.5614 7.44388 86.4791 7.44388C87.5163 7.44388 88.3258 7.68476 88.8895 8.16324C89.4679 8.64008 89.7497 9.28733 89.7497 10.146V10.9276H85.858C84.026 10.9276 82.6004 11.3127 81.6205 12.0714C80.6456 12.8399 80.1523 13.9558 80.1523 15.3912C80.1523 16.6824 80.6374 17.7164 81.596 18.4685C82.5578 19.2092 83.8376 19.5844 85.3992 19.5844C87.3066 19.5844 88.8174 18.906 89.8956 17.5656C89.9759 18.0998 90.1791 18.5259 90.5035 18.8356C90.9115 19.2239 91.5866 19.4222 92.5092 19.4222H94.918H97.4627V15.673L100.037 12.9726L104.519 19.3517L104.568 19.4222H107.643L101.862 11.1357L107.331 5.30548L107.327 5.30712ZM83.4247 13.6657C83.9064 13.3069 84.652 13.1233 85.6384 13.1233H89.7465V13.6346C89.7465 14.7538 89.3729 15.6681 88.6371 16.3531C87.8965 17.0249 86.8609 17.3641 85.5582 17.3641C84.6799 17.3641 83.9769 17.1658 83.4673 16.7758C82.9642 16.3908 82.7201 15.8943 82.7201 15.2568C82.7201 14.5391 82.9511 14.018 83.4247 13.6641V13.6657Z"
        fill="currentColor"
      />
      <path
        d="M75.8592 5.94697C74.8662 5.41442 73.7126 5.14404 72.4295 5.14404C71.1465 5.14404 69.9519 5.4521 68.9393 6.05839C67.925 6.6483 67.122 7.4971 66.5518 8.58351C66.0012 9.66664 65.7227 10.9382 65.7227 12.3638C65.7227 13.7894 66.0111 15.043 66.5813 16.1457C67.1696 17.2305 68.0003 18.0892 69.0507 18.6971C70.0978 19.2853 71.3349 19.5836 72.7261 19.5836C74.3254 19.5836 75.6986 19.1248 76.8063 18.2202C77.9124 17.2977 78.6137 16.1015 78.8906 14.6644L78.9283 14.4694H76.3606L76.3278 14.5907C76.105 15.4313 75.6527 16.1015 74.9858 16.5816C74.332 17.0453 73.5176 17.2813 72.5655 17.2813C71.3382 17.2813 70.3305 16.888 69.5734 16.1146C68.8147 15.3379 68.4116 14.2843 68.3772 12.9832V12.9586H79.0709L79.084 12.8079C79.12 12.3654 79.1381 12.0525 79.1381 11.8444C79.102 10.4892 78.7939 9.29139 78.2253 8.28036C77.6535 7.26606 76.8587 6.48116 75.8624 5.94533L75.8592 5.94697ZM76.4917 10.7956H68.5313C68.664 9.82558 69.0933 9.02266 69.8061 8.40654C70.5648 7.75109 71.4562 7.41845 72.4574 7.41845C73.5831 7.41845 74.5204 7.73306 75.243 8.35246C75.9411 8.93745 76.3606 9.7584 76.49 10.7956H76.4917Z"
        fill="currentColor"
      />
      <path
        d="M61.3784 6.00268C60.3674 5.43244 59.1941 5.14404 57.8914 5.14404C55.9808 5.14404 54.4274 5.86176 53.2705 7.27589L53.0067 5.30627H50.7798V24.8304H53.3246V17.6582C53.7539 18.1907 54.2995 18.6315 54.9501 18.9724H54.9533C55.7661 19.3804 56.7542 19.5852 57.893 19.5852C59.1794 19.5852 60.3526 19.2772 61.3833 18.6709C62.414 18.0629 63.2267 17.2043 63.797 16.1195C64.3836 15.0364 64.6818 13.773 64.6818 12.3654C64.6818 10.8841 64.3836 9.58471 63.797 8.50322C63.2251 7.41681 62.4124 6.57456 61.38 6.00432L61.3784 6.00268ZM62.0797 12.3654C62.0797 13.814 61.6668 15.0135 60.8507 15.9344C60.0577 16.8307 58.9893 17.2846 57.6751 17.2846C56.8263 17.2846 56.0545 17.0748 55.3827 16.6603C54.7272 16.2457 54.2111 15.6591 53.8489 14.9168C53.4835 14.1499 53.2967 13.2732 53.2967 12.3114C53.2967 11.3495 53.4819 10.5187 53.8473 9.7879C54.2094 9.0456 54.7256 8.46717 55.381 8.07062C56.0545 7.65605 56.8263 7.44631 57.6751 7.44631C58.9893 7.44631 60.0577 7.91004 60.8524 8.82603C61.6668 9.72727 62.0814 10.9185 62.0814 12.3654H62.0797Z"
        fill="currentColor"
      />
      <path
        d="M46.5305 11.6142C45.7488 11.3602 44.7378 11.157 43.5252 11.0128C42.6699 10.9063 42.0193 10.8064 41.5933 10.7179C41.2099 10.6343 40.8903 10.4754 40.6429 10.2443C40.4053 10.0231 40.289 9.70848 40.289 9.28244C40.289 8.70565 40.5479 8.25503 41.0837 7.90272C41.6294 7.52748 42.3635 7.3374 43.2614 7.3374C44.1594 7.3374 44.8787 7.56189 45.4604 8.00595C46.0422 8.45002 46.3666 8.99896 46.4518 9.68063L46.4698 9.82482H49.0081L48.995 9.64785C48.8835 8.19931 48.2887 7.06702 47.2269 6.2854C46.1929 5.51033 44.8329 5.11707 43.1811 5.11707C42.1717 5.11707 41.2476 5.30387 40.4381 5.67092C39.622 6.02322 38.9633 6.53283 38.4783 7.18664C38.0096 7.84209 37.772 8.60241 37.772 9.44466C37.772 10.3836 38.0227 11.139 38.5192 11.6929C39.0059 12.2172 39.6483 12.6039 40.4299 12.8464C41.1918 13.0824 42.1848 13.2758 43.3778 13.42C44.2151 13.5265 44.864 13.6346 45.3081 13.7411C45.7439 13.8411 46.0995 14.0279 46.365 14.2917C46.6206 14.5473 46.75 14.9259 46.75 15.4191C46.75 15.9959 46.4731 16.4547 45.9062 16.8217C45.3408 17.1806 44.5789 17.3641 43.6416 17.3641C42.583 17.3641 41.7064 17.1118 41.0362 16.6136C40.3856 16.1171 40.053 15.4945 40.0186 14.7096L40.012 14.5522H37.4722V14.7177C37.4918 16.2417 38.0686 17.4493 39.1878 18.308C40.3103 19.1551 41.8096 19.5845 43.6399 19.5845C44.6657 19.5845 45.6145 19.4075 46.46 19.0585C47.3121 18.7062 47.9987 18.1982 48.5017 17.5476C49.0081 16.8906 49.2653 16.1188 49.2653 15.2569C49.2653 14.2655 49.0064 13.4658 48.4968 12.8792C48.0101 12.2991 47.3481 11.8731 46.5288 11.6126L46.5305 11.6142Z"
        fill="currentColor"
      />
      <path
        d="M18.3886 23.6792L1.30926 21.2524L0 22.388L18.3886 24.9999L25.923 18.4635L24.6154 18.2783L18.3886 23.6792Z"
        fill="currentColor"
      />
      <path
        d="M18.3886 19.7167L25.923 13.1803L24.6154 12.9951L18.3886 18.3944L12.2946 17.5292L3.9245 16.3412L1.30926 15.9692L0 17.1048L2.61688 17.4751L0 19.7446L18.3886 22.3566L25.923 15.8217L24.6154 15.6366L18.3902 21.0375L9.6777 19.7987L10.9853 18.6647L18.3886 19.7167Z"
        fill="currentColor"
      />
      <path
        d="M18.3886 15.7523L1.30926 13.3255L0 14.4611L18.3886 17.073L25.923 10.5382L24.6154 10.353L18.3886 15.7523Z"
        fill="currentColor"
      />
      <path
        d="M24.6154 5.06775L21.9985 7.33724L18.3886 10.4686L11.3294 9.46581L1.30926 8.04185L0 9.17741L10.0202 10.5997L8.71255 11.7353L1.30762 10.6833L0 11.8189L18.3886 14.4308L25.923 7.89601L23.3061 7.52404L25.923 5.25291L24.6154 5.06775Z"
        fill="currentColor"
      />
      <path
        d="M25.923 2.61196L7.53438 0L0 6.53482L18.3886 9.14842L25.923 2.61196Z"
        fill="currentColor"
      />
    </svg>
  );
};

function AuthLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex flex-col justify-center items-center w-full md:w-1/2 min-h-screen p-8 md:p-16 bg-white relative">
      <div className="w-full flex flex-col items-center gap-8 max-w-xs">
        <div className="flex flex-col items-center gap-4">
          <GramLogo className="w-25 mb-2 dark:!invert-0" variant="vertical" />
          <p className="text-body-lg text-center dark:text-black">
            Create, Curate and Host high quality MCP servers for every use case.
            Enable AI to connect with your APIs.
          </p>
        </div>

        {children}
      </div>

      <div className="bottom-16 left-0 right-0 absolute flex justify-center items-center text-black dark:text-black">
        <Logo />
      </div>
    </div>
  );
}

export function LoginSection() {
  const [searchParams] = useSearchParams();
  const signinError = searchParams.get("signin_error");

  const handleLogin = async () => {
    window.location.href = `${getServerURL()}/rpc/auth.login`;
  };

  return (
    <AuthLayout>
      {signinError && (
        <p className="text-red-600 text-center mb-4">
          login error: {getAuthErrorMessage(signinError)}
        </p>
      )}

      <div className="relative z-10">
        <Button variant="brand" onClick={handleLogin}>Login</Button>
      </div>
    </AuthLayout>
  );
}

export function RegisterSection() {
  const [searchParams] = useSearchParams();
  const signinError = searchParams.get("signin_error");
  const telemetry = useTelemetry();
  const [companyName, setCompanyName] = useState("");
  const [validationError, setValidationError] = useState("");
  const sdk = useGramContext();

  const registerMutation = useMutation({
    mutationFn: async (vars: RegisterMutationVariables) => {
      await buildRegisterMutation(sdk).mutationFn(vars);

      const info = await authInfo(sdk);
      if (!info.ok) {
        throw info.error;
      }

      const org = info.value.result.organizations.find(
        (org) => org.id === info.value.result.activeOrganizationId
      );
      if (!org) {
        throw new Error("Organization not found");
      }

      return org;
    },

    onSuccess: () => {
      window.location.replace("/");
    },
    onError: (error) => {
      setValidationError(error.message);
    },
  });

  const handleCompanyNameChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const value = e.target.value;
    setCompanyName(value);

    // Clear previous errors
    setValidationError("");

    // Validate using the regex on type
    if (value.trim()) {
      const validOrgNameRegex = /^[a-zA-Z0-9\s-_]+$/;
      if (!validOrgNameRegex.test(value)) {
        setValidationError(
          "Company name contains invalid characters. Only letters, numbers, spaces, hyphens, and underscores are allowed."
        );
      }
    }
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();

    if (!companyName.trim()) {
      setValidationError("Company name is required");
      return;
    }

    telemetry.capture("onboarding_event", {
      action: "new_org_created",
      company_name: companyName,
      is_gram: true,
    });

    // Call the register mutation
    registerMutation.mutate({
      request: {
        registerRequestBody: {
          orgName: companyName.trim(),
        },
      },
    });
  };

  return (
    <AuthLayout>
      {signinError && (
        <p className="text-red-600 text-center mb-4">
          login error: {getAuthErrorMessage(signinError)}
        </p>
      )}

      <form onSubmit={handleSubmit} className="w-full flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <label
            htmlFor="companyName"
            className="text-sm font-medium text-gray-700 dark:text-gray-800"
          >
            Company Name
          </label>
          <input
            id="companyName"
            type="text"
            value={companyName}
            onChange={handleCompanyNameChange}
            placeholder="company name"
            className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent placeholder-gray-500 text-gray-900"
            disabled={registerMutation.isPending}
          />
        </div>

        {(validationError || registerMutation.error) && (
          <p className="text-red-600 text-sm text-center">
            {validationError || registerMutation.error?.message}
          </p>
        )}

        <div className="relative z-10">
          <Button
            variant="brand"
            type="submit"
            disabled={registerMutation.isPending || !companyName.trim()}
            className="w-full"
          >
            Create Organization
          </Button>
        </div>
      </form>
    </AuthLayout>
  );
}
