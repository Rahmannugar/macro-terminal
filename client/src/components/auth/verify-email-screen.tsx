import { type FormEvent, useState } from "react";
import { Link, Navigate, useLocation, useSearchParams } from "react-router";
import {
  useAccount,
  useResendVerification,
  useUpdateUsername,
  useVerifyEmail,
} from "../../hooks/use-account";
import { authenticationErrorMessage } from "../../lib/auth";
import { Input } from "../ui/input";
import { InputOTP, InputOTPGroup, InputOTPSlot } from "../ui/input-otp";
import { AuthField, AuthLayout } from "./auth-layout";

export function VerifyEmailScreen() {
  const account = useAccount();
  const verifyEmail = useVerifyEmail();
  const updateUsername = useUpdateUsername();
  const resend = useResendVerification();
  const [searchParams] = useSearchParams();
  const location = useLocation();
  const [email, setEmail] = useState(searchParams.get("email") ?? "");
  const [code, setCode] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);
  const [resendNote, setResendNote] = useState<string | null>(null);

  const pendingUsername =
    (location.state as { username?: string } | null)?.username?.trim() ?? "";

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
    verifyEmail.mutate(
      { email: trimmedEmail, code: code.trim() },
      {
        onSuccess: () => {
          if (pendingUsername) updateUsername.mutate({ username: pendingUsername });
        },
      },
    );
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
      subtitle="Enter the 6-digit code we sent you. Verifying signs you in."
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
            Resend the code
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
      <AuthField label="Verification code" htmlFor="verify-code">
        <InputOTP
          id="verify-code"
          autoComplete="one-time-code"
          maxLength={6}
          value={code}
          onChange={setCode}
        >
          <InputOTPGroup>
            <InputOTPSlot index={0} />
            <InputOTPSlot index={1} />
            <InputOTPSlot index={2} />
            <InputOTPSlot index={3} />
            <InputOTPSlot index={4} />
            <InputOTPSlot index={5} />
          </InputOTPGroup>
        </InputOTP>
      </AuthField>
    </AuthLayout>
  );
}
