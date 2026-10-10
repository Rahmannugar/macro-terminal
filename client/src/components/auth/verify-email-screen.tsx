import { type FormEvent, useState } from "react";
import { Link, Navigate, useSearchParams } from "react-router";
import { useAccount, useResendVerification, useVerifyEmail } from "../../hooks/use-account";
import { authenticationErrorMessage } from "../../lib/auth";
import { Input } from "../ui/input";
import { AuthField, AuthLayout } from "./auth-layout";

export function VerifyEmailScreen() {
  const account = useAccount();
  const verifyEmail = useVerifyEmail();
  const resend = useResendVerification();
  const [searchParams] = useSearchParams();
  const [email, setEmail] = useState(searchParams.get("email") ?? "");
  const [code, setCode] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);
  const [resendNote, setResendNote] = useState<string | null>(null);

  if (account.isSuccess) return <Navigate to="/" replace />;

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmedEmail = email.trim();
    if (!trimmedEmail?.includes("@")) {
      setLocalError("Enter a valid email address.");
      return;
    }
    if (!/^\d{6}$/.test(code.trim())) {
      setLocalError("Enter the 6-digit code from your email.");
      return;
    }
    setLocalError(null);
    setResendNote(null);
    verifyEmail.mutate({ email: trimmedEmail, code: code.trim() });
  }

  function resendCode() {
    const trimmedEmail = email.trim();
    if (!trimmedEmail?.includes("@")) {
      setLocalError("Enter a valid email address.");
      return;
    }
    setLocalError(null);
    resend.mutate(
      { email: trimmedEmail },
      {
        onSuccess: () =>
          setResendNote("If that address needs verification, a new code is on its way."),
        onError: (error) => setResendNote(authenticationErrorMessage(error)),
      },
    );
  }

  const errorMessage =
    localError ?? (verifyEmail.isError ? authenticationErrorMessage(verifyEmail.error) : null);

  return (
    <AuthLayout
      title="Verify your email"
      subtitle="Enter the one-time code we emailed you. Verification signs you in."
      errorMessage={errorMessage}
      pending={verifyEmail.isPending}
      submitLabel="Verify"
      onSubmit={submit}
      footer={
        <>
          {resendNote ? <span className="block">{resendNote}</span> : null}
          <button
            type="button"
            onClick={resendCode}
            disabled={resend.isPending}
            className="mt-2 inline text-primary hover:underline disabled:opacity-60"
          >
            {resend.isPending ? "Sending…" : "Resend the code"}
          </button>
          <span className="block">
            Wrong address?{" "}
            <Link to="/sign-up" className="font-medium text-primary hover:underline">
              Start over
            </Link>
          </span>
        </>
      }
    >
      <AuthField label="Email" htmlFor="verify-email">
        <Input
          id="verify-email"
          type="email"
          autoComplete="email"
          placeholder="trader@email.com"
          value={email}
          onChange={(event) => setEmail(event.target.value)}
        />
      </AuthField>
      <AuthField label="One-time code" htmlFor="verify-code">
        <Input
          id="verify-code"
          inputMode="numeric"
          autoComplete="one-time-code"
          maxLength={6}
          placeholder="123456"
          value={code}
          onChange={(event) => setCode(event.target.value)}
        />
      </AuthField>
    </AuthLayout>
  );
}
