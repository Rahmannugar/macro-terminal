import { type QueryKey, useMutation, useQueryClient } from "@tanstack/react-query";
import { useAdminList } from "./admin";
import { apiRequest } from "./api";

export type EntityPair = {
  id: string;
  symbol: string;
  baseEntityId: string;
  quoteEntityId: string;
  createdAt: string;
  updatedAt: string;
};

export const watchListKey: QueryKey = ["assets"];
export const pairCatalogKey: QueryKey = ["catalog", "entity-pairs"];

export function useFollowedPairs() {
  return useAdminList<EntityPair>({
    key: watchListKey,
    path: "/api/v1/assets",
    rowsKey: "entityPairs",
  });
}

export function usePairCatalog() {
  return useAdminList<EntityPair>({
    key: pairCatalogKey,
    path: "/api/v1/entity-pairs",
    rowsKey: "entityPairs",
  });
}

export function useToggleFollow() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ pairId, following }: { pairId: string; following: boolean }) =>
      following
        ? apiRequest("/api/v1/assets", {
            method: "POST",
            body: JSON.stringify({ entityPairId: pairId }),
          })
        : apiRequest(`/api/v1/assets/${pairId}`, { method: "DELETE" }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: watchListKey });
      queryClient.invalidateQueries({ queryKey: pairCatalogKey });
      queryClient.invalidateQueries({ queryKey: ["feed"] });
    },
  });
}
