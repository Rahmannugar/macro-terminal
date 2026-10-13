import { useState } from "react";
import { AdminScreen } from "../components/admin/admin-screen";
import { type ChartCandle, MarketChart } from "../components/market-chart";
import { PairSelect } from "../components/pair-select";
import { Button } from "../components/ui/button";
import { Skeleton } from "../components/ui/skeleton";
import { adminErrorMessage, adminRows } from "../lib/admin";
import {
  candlesAscending,
  type Timeframe,
  timeframeLabels,
  timeframeMarks,
  timeframes,
  useCandles,
} from "../lib/market";
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
      <div className="flex flex-wrap items-end gap-x-6 gap-y-4">
        <div className="w-full max-w-[280px]">
          <span className="text-sm font-medium">Pair</span>
          <div className="mt-1.5">
            <PairSelect
              label="Pair"
              value={activePairId}
              options={pairs}
              onChange={setPairId}
              loading={catalog.isPending}
              error={catalog.isError ? catalog.error : undefined}
            />
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <span className="text-sm font-medium">Interval</span>
          <div className="inline-flex items-center rounded-lg border border-border bg-card p-1">
            {timeframes.map((option) => (
              <button
                key={option}
                type="button"
                aria-pressed={timeframe === option}
                aria-label={timeframeLabels[option]}
                className={`h-9 min-w-9 rounded-md px-2.5 text-xs font-semibold transition-colors ${
                  timeframe === option
                    ? "bg-primary text-white"
                    : "text-muted-foreground hover:bg-secondary hover:text-foreground"
                }`}
                onClick={() => setTimeframe(option)}
              >
                {timeframeMarks[option]}
              </button>
            ))}
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
              ? `No ${timeframeLabels[timeframe].toLowerCase()} candles are stored for ${activePair.symbol} yet.`
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
