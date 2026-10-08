import { type QueryKey, useInfiniteQuery } from "@tanstack/react-query";
import { APIError, apiRequest } from "./api";

export type Timeframe = "1min" | "1day";

export type Candle = {
  id: string;
  entityPairId: string;
  timeframe: string;
  timestamp: string;
  open: number;
  high: number;
  low: number;
  close: number;
};

export type CandlePage = { candles: Candle[]; nextCursor: string | null };

export const timeframes: Timeframe[] = ["1min", "1day"];

export function candlesKey(pairId: string, timeframe: Timeframe): QueryKey {
  return ["candles", pairId, timeframe];
}

export async function requestCandlePage(
  pairId: string,
  timeframe: Timeframe,
  cursor: string | null,
): Promise<CandlePage> {
  const parameters = new URLSearchParams();
  parameters.set("entityPairId", pairId);
  parameters.set("timeframe", timeframe);
  parameters.set("limit", "100");
  if (cursor) parameters.set("cursor", cursor);
  const payload = await apiRequest(`/api/v1/candles?${parameters.toString()}`);
  if (typeof payload !== "object" || payload === null) {
    throw new APIError(
      502,
      "invalid_response",
      "Macro Terminal received an unexpected candles response.",
    );
  }
  const record = payload as Record<string, unknown>;
  if (!Array.isArray(record.candles)) {
    throw new APIError(
      502,
      "invalid_response",
      "Macro Terminal received an unexpected candles response.",
    );
  }
  return {
    candles: record.candles as Candle[],
    nextCursor: typeof record.nextCursor === "string" ? record.nextCursor : null,
  };
}

export function useCandles(pairId: string | null, timeframe: Timeframe) {
  return useInfiniteQuery<
    CandlePage,
    APIError,
    { pages: CandlePage[]; pageParams: (string | null)[] },
    QueryKey,
    string | null
  >({
    queryKey: candlesKey(pairId ?? "", timeframe),
    enabled: Boolean(pairId),
    initialPageParam: null,
    queryFn: ({ pageParam }) => requestCandlePage(pairId as string, timeframe, pageParam),
    getNextPageParam: (page) => page.nextCursor,
    staleTime: 15_000,
    retry: false,
  });
}

export function candlesAscending(query: { data?: { pages: CandlePage[] } }): Candle[] {
  return query.data?.pages.flatMap((page) => page.candles).reverse() ?? [];
}
