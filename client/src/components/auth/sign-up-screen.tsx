import { type FormEvent, useState } from "react";
import { Link, Navigate, useNavigate } from "react-router";
import { useAccount, useSignUp } from "../../hooks/use-account";
import { authenticationErrorMessage } from "../../lib/auth";
import { Input, PasswordInput } from "../ui/input";
import { AuthField, AuthLayout } from "./auth-layout";

export function SignUpScreen() {
  const account = useAccount();
  const signUp = useSignUp();
  const navigate = useNavigate();
  const [email, setEmail] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);

  if (account.isSuccess) return <Navigate to="/" replace />;

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmedEmail = email.trim();
    const trimmedUsername = username.trim();
    if (!trimmedEmail?.includes("@")) {
      setLocalError("Enter a valid email address.");
      return;
    }
    if (trimmedUsername.length < 2) {
      setLocalError("Username must be at least 2 characters.");
      return;
    }
    if (password.length < 8) {
      setLocalError("Password must be at least 8 characters.");
      return;
    }
    setLocalError(null);
    signUp.mutate(
      { email: trimmedEmail, password },
      {
        onSuccess: () => {
          navigate(`/verify-email?email=${encodeURIComponent(trimmedEmail)}`, {
            replace: true,
            state: { username: trimmedUsername },
          });
        },
      },
    );
  }

  const errorMessage =
    localError ?? (signUp.isError ? authenticationErrorMessage(signUp.error) : null);

  return (
    <AuthLayout
      title="Create your account"
      subtitle="We email you a one-time code to verify your address."
      errorMessage={errorMessage}
      pending={signUp.isPending}
      submitLabel="Create account"
      onSubmit={submit}
      footer={
        <>
          Already have an account?{" "}
          <Link to="/sign-in" className="font-medium text-primary hover:underline">
            Sign in
          </Link>
        </>
      }
    >
      <AuthField label="Username" htmlFor="sign-up-username">
        <Input
          id="sign-up-username"
          autoComplete="username"
          placeholder="how you'll appear in the terminal"
          value={username}
          onChange={(event) => setUsername(event.target.value)}
        />
      </AuthField>
      <AuthField label="Email" htmlFor="sign-up-email">
        <Input
          id="sign-up-email"
          type="email"
          autoComplete="email"
          placeholder="trader@email.com"
          value={email}
          onChange={(event) => setEmail(event.target.value)}
        />
      </AuthField>
      <AuthField label="Password" htmlFor="sign-up-password">
        <PasswordInput
          id="sign-up-password"
          autoComplete="new-password"
          value={password}
          onChange={(event) => setPassword(event.target.value)}
        />
      </AuthField>
    </AuthLayout>
  );
}
