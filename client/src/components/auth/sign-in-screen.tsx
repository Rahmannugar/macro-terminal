import { type FormEvent, useState } from "react";
import { Link, Navigate, useNavigate } from "react-router";
import { useAccount, useSignIn } from "../../hooks/use-account";
import { authenticationErrorMessage, requiresVerification } from "../../lib/auth";
import { Input, PasswordInput } from "../ui/input";
import { AuthField, AuthLayout } from "./auth-layout";

export function SignInScreen() {
  const account = useAccount();
  const signIn = useSignIn();
  const navigate = useNavigate();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);

  if (account.isSuccess) return <Navigate to="/" replace />;

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmedEmail = email.trim();
    if (!trimmedEmail?.includes("@")) {
      setLocalError("Enter a valid email address.");
      return;
    }
    if (!password) {
      setLocalError("Enter your password.");
      return;
    }
    setLocalError(null);
    signIn.mutate(
      { email: trimmedEmail, password },
      {
        onError: (error) => {
          if (requiresVerification(error)) {
            navigate(`/verify-email?email=${encodeURIComponent(trimmedEmail)}`);
          }
        },
      },
    );
  }

  const errorMessage =
    localError ?? (signIn.isError ? authenticationErrorMessage(signIn.error) : null);

  return (
    <AuthLayout
      title="Sign in"
      subtitle="Macro intelligence and fundamental research terminal."
      errorMessage={errorMessage}
      pending={signIn.isPending}
      submitLabel="Sign in"
      onSubmit={submit}
      footer={
        <>
          New to Macro Terminal?{" "}
          <Link to="/sign-up" className="font-medium text-primary hover:underline">
            Create an account
          </Link>
        </>
      }
    >
      <AuthField label="Email" htmlFor="sign-in-email">
        <Input
          id="sign-in-email"
          type="email"
          autoComplete="email"
          placeholder="trader@email.com"
          value={email}
          onChange={(event) => setEmail(event.target.value)}
        />
      </AuthField>
      <AuthField label="Password" htmlFor="sign-in-password">
        <PasswordInput
          id="sign-in-password"
          autoComplete="current-password"
          value={password}
          onChange={(event) => setPassword(event.target.value)}
        />
      </AuthField>
      <div className="text-right">
        <Link to="/forgot-password" className="text-sm text-primary hover:underline">
          Forgot password?
        </Link>
      </div>
    </AuthLayout>
  );
}
