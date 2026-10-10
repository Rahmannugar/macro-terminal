import { type FormEvent, useState } from "react";
import { Link, Navigate } from "react-router";
import { useAccount, useForgotPassword } from "../../hooks/use-account";
import { authenticationErrorMessage } from "../../lib/auth";
import { Input } from "../ui/input";
import { AuthField, AuthLayout } from "./auth-layout";

export function ForgotPasswordScreen() {
  const account = useAccount();
  const forgotPassword = useForgotPassword();
  const [email, setEmail] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);
  const [sentNote, setSentNote] = useState<string | null>(null);

  if (account.isSuccess) return <Navigate to="/" replace />;

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmedEmail = email.trim();
    if (!trimmedEmail?.includes("@")) {
      setLocalError("Enter a valid email address.");
      return;
    }
    setLocalError(null);
    setSentNote(null);
    forgotPassword.mutate(
      { email: trimmedEmail },
      {
        onSuccess: () =>
          setSentNote("If that address has an account, a reset link is on its way."),
      },
    );
  }

  const errorMessage =
    localError ??
    (forgotPassword.isError ? authenticationErrorMessage(forgotPassword.error) : null);

  return (
    <AuthLayout
      title="Forgot your password?"
      subtitle="Enter your email and we'll send you a reset link."
      errorMessage={errorMessage}
      pending={forgotPassword.isPending}
      submitLabel="Send reset link"
      onSubmit={submit}
      footer={
        <>
          {sentNote ? <span className="block">{sentNote}</span> : null}
          <span className="block">
            Remembered it?{" "}
            <Link to="/sign-in" className="font-medium text-primary hover:underline">
              Sign in
            </Link>
          </span>
        </>
      }
    >
      <AuthField label="Email" htmlFor="forgot-password-email">
        <Input
          id="forgot-password-email"
          type="email"
          autoComplete="email"
          placeholder="trader@email.com"
          value={email}
          onChange={(event) => setEmail(event.target.value)}
        />
      </AuthField>
    </AuthLayout>
  );
}
