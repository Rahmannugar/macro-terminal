import { useState } from "react";
import { AdminScreen } from "../components/admin/admin-screen";
import { type ChartCandle, MarketChart } from "../components/market-chart";
import { Button } from "../components/ui/button";
import { Select } from "../components/ui/select";
import { Skeleton } from "../components/ui/skeleton";
import { Tabs } from "../components/ui/tabs";
import { adminErrorMessage, adminRows } from "../lib/admin";
import { candlesAscending, type Timeframe, timeframes, useCandles } from "../lib/market";
import { usePairCatalog } from "../lib/watchlist";

export function MarketRoute() {
  const catalog = usePairCatalog();
  const [pairId, setPairId] = useState<string | null>(null);
  const [timeframe, setTimeframe] = useState<Timeframe>("1min");

  const pairs = adminRows(catalog).toSorted((a, b) => a.symbol.localeCompare(b.symbol));
  const activePairId = pairId ?? pairs[0]?.id ?? null;
  const activePair = pairs.find((pair) => pair.id === activePairId);
  const candles = useCandles(activePairId, timeframe);

  const chartData: ChartCandle[] = candlesAscending(candles).map((candle) => ({
    time: Math.floor(Date.parse(candle.timestamp) / 1000) as ChartCandle["time"],
    open: candle.open,
    high: candle.high,
    low: candle.low,
    close: candle.close,
  }));

  return (
    <AdminScreen
      title="Market"
      description="Price history for the pairs Macro Terminal tracks, updated continuously from live market data."
    >
      <div className="flex flex-wrap items-end gap-3">
        <div className="w-full max-w-[280px]">
          <label className="text-sm font-medium" htmlFor="market-pair">
            Pair
          </label>
          <Select
            id="market-pair"
            className="mt-1.5"
            value={activePairId ?? ""}
            disabled={catalog.isPending || pairs.length === 0}
            onChange={(event) => setPairId(event.target.value)}
          >
            {pairs.length === 0 ? <option value="">No pairs available</option> : null}
            {pairs.map((pair) => (
              <option key={pair.id} value={pair.id}>
                {pair.symbol}
              </option>
            ))}
          </Select>
        </div>
        <div>
          <span className="text-sm font-medium">Interval</span>
          <div className="mt-1.5">
            <Tabs
              value={timeframe}
              options={timeframes}
              onChange={setTimeframe}
              label={(option) => (option === "1min" ? "1 minute" : "1 day")}
            />
          </div>
        </div>
      </div>

      <div className="mt-6 rounded-xl border border-border bg-card p-4">
        {catalog.isError ? (
          <p className="py-16 text-center text-sm text-muted-foreground">
            {adminErrorMessage(catalog.error)}
          </p>
        ) : candles.isError ? (
          <div className="py-16 text-center">
            <p className="text-sm text-muted-foreground">{adminErrorMessage(candles.error)}</p>
            <Button
              variant="secondary"
              className="mt-4 h-9 px-4 text-sm"
              onClick={() => candles.refetch()}
            >
              Retry
            </Button>
          </div>
        ) : candles.isPending ? (
          <Skeleton className="h-[440px] rounded-lg" />
        ) : chartData.length === 0 ? (
          <p className="py-16 text-center text-sm text-muted-foreground">
            {activePair
              ? `No ${timeframe === "1min" ? "minute" : "daily"} candles are stored for ${activePair.symbol} yet.`
              : "Pick a pair to see its price history."}
          </p>
        ) : (
          <>
            <MarketChart candles={chartData} timeframe={timeframe} />
            {candles.hasNextPage || candles.isFetchingNextPage ? (
              <div className="mt-3 flex min-h-10 items-center justify-center">
                <Button
                  variant="secondary"
                  className="h-10 px-5 text-sm"
                  pending={candles.isFetchingNextPage}
                  onClick={() => candles.fetchNextPage()}
                >
                  Load older candles
                </Button>
              </div>
            ) : null}
          </>
        )}
      </div>
    </AdminScreen>
  );
}
