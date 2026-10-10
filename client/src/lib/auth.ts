import { APIError, apiRequest } from "./api";

export type Account = {
  session: { createdAt: string; expiresAt: string };
  account: { id: string; username: string | null; email: string; role: string; status: string };
};

export const authenticationQueryKey = ["authentication", "account"] as const;

export async function signUp(email: string, password: string): Promise<void> {
  await apiRequest("/auth/sign-up", {
    method: "POST",
    body: JSON.stringify({ email, password }),
  });
}

export async function signIn(email: string, password: string): Promise<void> {
  await apiRequest("/auth/sign-in", {
    method: "POST",
    body: JSON.stringify({ email, password }),
  });
}

export async function verifyEmail(email: string, code: string): Promise<void> {
  await apiRequest("/auth/verify-email", {
    method: "POST",
    body: JSON.stringify({ email, code }),
  });
}

export async function resendVerification(email: string): Promise<void> {
  await apiRequest("/auth/resend-verification", {
    method: "POST",
    body: JSON.stringify({ email }),
  });
}

export async function forgotPassword(email: string): Promise<void> {
  await apiRequest("/auth/forgot-password", {
    method: "POST",
    body: JSON.stringify({ email }),
  });
}

export async function resetPassword(token: string, newPassword: string): Promise<void> {
  await apiRequest("/auth/reset-password", {
    method: "POST",
    body: JSON.stringify({ token, newPassword }),
  });
}

export async function signOut(): Promise<void> {
  await apiRequest("/auth/sign-out", { method: "POST" });
}

export async function updateUsername(username: string): Promise<Account> {
  const response = await apiRequest("/api/v1/account", {
    method: "PATCH",
    body: JSON.stringify({ username }),
  });
  if (!isAccount(response)) {
    throw new APIError(
      502,
      "invalid_response",
      "Macro Terminal returned an unexpected account response.",
    );
  }
  return response;
}

export async function loadAccount(): Promise<Account> {
  const response = await apiRequest("/api/v1/account");
  if (!isAccount(response)) {
    throw new APIError(
      502,
      "invalid_response",
      "Macro Terminal returned an unexpected account response.",
    );
  }
  return response;
}

export function authenticationErrorMessage(error: unknown): string {
  if (!(error instanceof APIError)) {
    return "Macro Terminal could not complete the request. Try again.";
  }
  switch (error.code) {
    case "invalid_credentials":
      return "The email or password is incorrect.";
    case "email_not_verified":
      return "Verify your email before signing in.";
    case "invalid_token":
      return "This code is invalid or has expired.";
    case "registration_unavailable":
      return "An account cannot be created with that email.";
    case "invalid_password":
    case "invalid_request":
      return "Review the information and try again.";
    case "too_many_attempts":
      return "Too many attempts. Wait a moment and try again.";
    case "origin_not_allowed":
      return "This application is not allowed to make authentication requests.";
    default:
      return error.message;
  }
}

export function requiresVerification(error: unknown): boolean {
  return error instanceof APIError && error.code === "email_not_verified";
}

function isAccount(value: unknown): value is Account {
  if (typeof value !== "object" || value === null) return false;
  const record = value as Record<string, unknown>;
  const session = record.session;
  const account = record.account;
  if (typeof session !== "object" || session === null) return false;
  if (typeof account !== "object" || account === null) return false;
  const sessionFields = session as Record<string, unknown>;
  const accountFields = account as Record<string, unknown>;
  return (
    typeof sessionFields.createdAt === "string" &&
    typeof sessionFields.expiresAt === "string" &&
    typeof accountFields.id === "string" &&
    (accountFields.username === null || typeof accountFields.username === "string") &&
    typeof accountFields.email === "string" &&
    typeof accountFields.role === "string" &&
    typeof accountFields.status === "string"
  );
}
