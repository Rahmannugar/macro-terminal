import {
  type QueryKey,
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { APIError, apiRequest } from "./api";

export type AdminListPage<T> = { rows: T[]; nextCursor: string | null };

type AdminListSpec = {
  key: QueryKey;
  path: string;
  rowsKey: string;
  filters?: Record<string, string>;
  enabled?: boolean;
};

export async function requestAdminPage<T>(
  path: string,
  rowsKey: string,
  filters: Record<string, string> | undefined,
  cursor: string | null,
  limit = "25",
): Promise<AdminListPage<T>> {
  const parameters = new URLSearchParams();
  parameters.set("limit", limit);
  if (cursor) parameters.set("cursor", cursor);
  for (const [name, value] of Object.entries(filters ?? {})) {
    if (value) parameters.set(name, value);
  }
  const payload = await apiRequest(`${path}?${parameters.toString()}`);
  if (typeof payload !== "object" || payload === null) {
    throw new APIError(
      502,
      "invalid_response",
      "Macro Terminal received an unexpected list response.",
    );
  }
  const record = payload as Record<string, unknown>;
  const rows = record[rowsKey];
  if (!Array.isArray(rows)) {
    throw new APIError(
      502,
      "invalid_response",
      "Macro Terminal received an unexpected list response.",
    );
  }
  return {
    rows: rows as T[],
    nextCursor: typeof record.nextCursor === "string" ? record.nextCursor : null,
  };
}

export function useAdminList<T extends { id: string }>({
  key,
  path,
  rowsKey,
  filters,
  enabled,
}: AdminListSpec) {
  const queryKey: QueryKey = [...key, filters ?? {}];
  return useInfiniteQuery<
    AdminListPage<T>,
    APIError,
    { pages: AdminListPage<T>[]; pageParams: (string | null)[] },
    QueryKey,
    string | null
  >({
    queryKey,
    enabled: enabled ?? true,
    initialPageParam: null,
    queryFn: ({ pageParam }) => requestAdminPage<T>(path, rowsKey, filters, pageParam),
    getNextPageParam: (page) => page.nextCursor,
    staleTime: 15_000,
    retry: false,
  });
}

export function adminRows<T>(query: { data?: { pages: AdminListPage<T>[] } }): T[] {
  return query.data?.pages.flatMap((page) => page.rows) ?? [];
}

export function useAdminCreate<TBody>(key: QueryKey, path: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: TBody) =>
      apiRequest(path, { method: "POST", body: JSON.stringify(body) }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: key }),
  });
}

export function useAdminPost(key: QueryKey) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (path: string) => apiRequest(path, { method: "POST" }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: key }),
  });
}

export function useAdminReplace<TBody>(key: QueryKey, path: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: TBody) =>
      apiRequest(path, { method: "PATCH", body: JSON.stringify(body) }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: key }),
  });
}

export function useReferenceRows<T>(key: QueryKey, path: string, rowsKey: string): T[] {
  const query = useQuery<AdminListPage<T>, APIError>({
    queryKey: key,
    queryFn: () => requestAdminPage<T>(path, rowsKey, undefined, null, "100"),
    staleTime: 60_000,
    retry: false,
  });
  return query.data?.rows ?? [];
}

export function adminErrorMessage(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof APIError) return error.message;
  return "The request could not be completed.";
}
