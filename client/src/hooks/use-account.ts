import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { APIError } from "../lib/api";
import {
  type Account,
  authenticationQueryKey,
  forgotPassword,
  loadAccount,
  resendVerification,
  resetPassword,
  signIn,
  signOut,
  signUp,
  updateUsername,
  verifyEmail,
} from "../lib/auth";

export function useAccount() {
  return useQuery<Account, APIError>({
    queryKey: authenticationQueryKey,
    queryFn: loadAccount,
    retry: false,
    staleTime: 30_000,
  });
}

export function useSignIn() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ email, password }: { email: string; password: string }) =>
      signIn(email, password),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: authenticationQueryKey }),
  });
}

export function useSignUp() {
  return useMutation({
    mutationFn: ({ email, password }: { email: string; password: string }) =>
      signUp(email, password),
  });
}

export function useVerifyEmail() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ email, code }: { email: string; code: string }) => verifyEmail(email, code),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: authenticationQueryKey }),
  });
}

export function useResendVerification() {
  return useMutation({
    mutationFn: ({ email }: { email: string }) => resendVerification(email),
  });
}

export function useForgotPassword() {
  return useMutation({
    mutationFn: ({ email }: { email: string }) => forgotPassword(email),
  });
}

export function useResetPassword() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ token, newPassword }: { token: string; newPassword: string }) =>
      resetPassword(token, newPassword),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: authenticationQueryKey }),
  });
}

export function useSignOut() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: signOut,
    onSettled: () => queryClient.removeQueries({ queryKey: authenticationQueryKey }),
  });
}

export function useUpdateUsername() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ username }: { username: string }) => updateUsername(username),
    onSuccess: (data) => queryClient.setQueryData(authenticationQueryKey, data),
  });
}
