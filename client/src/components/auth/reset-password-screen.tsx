import { type FormEvent, useState } from "react";
import { Link, Navigate, useNavigate, useSearchParams } from "react-router";
import { useAccount, useResetPassword } from "../../hooks/use-account";
import { authenticationErrorMessage } from "../../lib/auth";
import { PasswordInput } from "../ui/input";
import { AuthField, AuthLayout } from "./auth-layout";

export function ResetPasswordScreen() {
  const account = useAccount();
  const resetPassword = useResetPassword();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const token = searchParams.get("token") ?? "";
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);

  if (account.isSuccess) return <Navigate to="/" replace />;

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!token) {
      setLocalError("This reset link is invalid. Request a new one.");
      return;
    }
    if (password.length < 8) {
      setLocalError("Use at least 8 characters for your password.");
      return;
    }
    if (password !== confirmation) {
      setLocalError("Passwords do not match.");
      return;
    }
    setLocalError(null);
    resetPassword.mutate(
      { token, newPassword: password },
      { onSuccess: () => navigate("/sign-in", { replace: true }) },
    );
  }

  const errorMessage =
    localError ??
    (resetPassword.isError ? authenticationErrorMessage(resetPassword.error) : null);

  return (
    <AuthLayout
      title="Set a new password"
      subtitle="Choose a new password for your Macro Terminal account."
      errorMessage={errorMessage}
      pending={resetPassword.isPending}
      submitLabel="Reset password"
      onSubmit={submit}
      footer={
        <>
          Remembered it?{" "}
          <Link to="/sign-in" className="font-medium text-primary hover:underline">
            Sign in
          </Link>
        </>
      }
    >
      <AuthField label="New password" htmlFor="reset-password">
        <PasswordInput
          id="reset-password"
          autoComplete="new-password"
          value={password}
          onChange={(event) => setPassword(event.target.value)}
        />
      </AuthField>
      <AuthField label="Confirm password" htmlFor="reset-password-confirm">
        <PasswordInput
          id="reset-password-confirm"
          autoComplete="new-password"
          value={confirmation}
          onChange={(event) => setConfirmation(event.target.value)}
        />
      </AuthField>
    </AuthLayout>
  );
}
