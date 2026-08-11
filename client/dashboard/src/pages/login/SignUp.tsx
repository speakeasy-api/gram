import { AuthShell } from "./components/auth-shell";
import { SignUpPanel } from "./components/signup-panel";

export default function SignUp(): JSX.Element {
  return (
    <AuthShell page="Sign up" contentClassName="max-w-[400px] gap-5">
      <SignUpPanel />
    </AuthShell>
  );
}
