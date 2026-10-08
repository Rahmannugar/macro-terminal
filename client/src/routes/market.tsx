import { useState } from "react";
import { Bar, ComposedChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { AdminScreen } from "../components/admin/admin-screen";
import { Button } from "../components/ui/button";
import { Select } from "../components/ui/select";
import { adminErrorMessage, adminRows } from "../lib/admin";
import { candlesAscending, type Timeframe, timeframes, useCandles } from "../lib/market";
import { usePairCatalog } from "../lib/watchlist";

type ChartCandle = { t: number; o: number; h: number; l: number; c: number };

const upColor = "#087cec";
const downColor = "#e5484d";
const chartHeight = 440;

export function MarketRoute() {
  const catalog = usePairCatalog();
  const [pairId, setPairId] = useState<string | null>(null);
  const [timeframe, setTimeframe] = useState<Timeframe>("1min");

  const pairs = adminRows(catalog).toSorted((a, b) => a.symbol.localeCompare(b.symbol));
  const activePairId = pairId ?? pairs[0]?.id ?? null;
  const activePair = pairs.find((pair) => pair.id === activePairId);
  const candles = useCandles(activePairId, timeframe);

  const chartData: ChartCandle[] = candlesAscending(candles).map((candle) => ({
    t: Date.parse(candle.timestamp),
    o: candle.open,
    h: candle.high,
    l: candle.low,
    c: candle.close,
  }));
  const priceDomain = domainOf(chartData);

  return (
    <AdminScreen
      title="Market"
      description="Price history for the pairs Macro Terminal tracks, straight from the candle store."
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
          <div className="mt-1.5 flex gap-2">
            {timeframes.map((option) => (
              <Button
                key={option}
                type="button"
                variant={timeframe === option ? "primary" : "secondary"}
                className="h-11 px-4 text-sm"
                aria-pressed={timeframe === option}
                onClick={() => setTimeframe(option)}
              >
                {option === "1min" ? "1 minute" : "1 day"}
              </Button>
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
          <p className="py-16 text-center text-sm text-muted-foreground">Loading…</p>
        ) : chartData.length === 0 ? (
          <p className="py-16 text-center text-sm text-muted-foreground">
            {activePair
              ? `No ${timeframe === "1min" ? "minute" : "daily"} candles are stored for ${activePair.symbol} yet.`
              : "Pick a pair to see its price history."}
          </p>
        ) : (
          <>
            <ResponsiveContainer width="100%" height={chartHeight}>
              <ComposedChart data={chartData} barCategoryGap="20%">
                <XAxis
                  type="number"
                  dataKey="t"
                  domain={["dataMin", "dataMax"]}
                  tickFormatter={(value: number) => formatAxisTime(Number(value), timeframe)}
                  tickLine={false}
                  axisLine={{ stroke: "var(--border)" }}
                  minTickGap={64}
                  tick={{ fontSize: 11, fill: "var(--muted-foreground)" }}
                />
                <YAxis
                  orientation="right"
                  domain={priceDomain}
                  tickFormatter={(value: number) => formatPrice(Number(value))}
                  tickLine={false}
                  axisLine={false}
                  width={72}
                  tick={{ fontSize: 11, fill: "var(--muted-foreground)" }}
                />
                <Tooltip
                  cursor={{ stroke: "var(--border)" }}
                  content={<CandleTooltip timeframe={timeframe} />}
                />
                <Bar
                  dataKey={(entry: ChartCandle) => [entry.l, entry.h]}
                  shape={<CandleShape />}
                  isAnimationActive={false}
                />
              </ComposedChart>
            </ResponsiveContainer>
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

function CandleShape(props: {
  x?: number;
  y?: number;
  width?: number;
  height?: number;
  payload?: ChartCandle;
}) {
  const { x, y, width, height, payload } = props;
  if (x == null || y == null || width == null || height == null || !payload) return null;
  const span = payload.h - payload.l;
  const scale = span > 0 ? height / span : 0;
  const bodyTop = y + (payload.h - Math.max(payload.o, payload.c)) * scale;
  const bodyHeight = span > 0 ? Math.abs(payload.c - payload.o) * scale : Math.max(height, 1);
  const rising = payload.c >= payload.o;
  const color = rising ? upColor : downColor;
  const center = x + width / 2;
  const bodyWidth = Math.max(width * 0.6, 1);
  return (
    <g>
      <line x1={center} x2={center} y1={y} y2={y + height} stroke={color} strokeWidth={1} />
      <rect
        x={x + (width - bodyWidth) / 2}
        y={bodyTop}
        width={bodyWidth}
        height={Math.max(bodyHeight, 1)}
        fill={color}
        rx={1}
      />
    </g>
  );
}

function CandleTooltip({
  active,
  payload,
  timeframe,
}: {
  active?: boolean;
  payload?: ReadonlyArray<{ payload?: ChartCandle }>;
  timeframe: Timeframe;
}) {
  const entry = payload?.[0]?.payload;
  if (!active || !entry) return null;
  return (
    <div className="rounded-lg border border-border bg-card px-3 py-2 text-xs shadow-sm">
      <p className="font-medium">{formatAxisTime(entry.t, timeframe)}</p>
      <p className="mt-1 text-muted-foreground">
        O {formatPrice(entry.o)} · H {formatPrice(entry.h)} · L {formatPrice(entry.l)} · C{" "}
        {formatPrice(entry.c)}
      </p>
    </div>
  );
}

function domainOf(data: ChartCandle[]): [number, number] {
  if (data.length === 0) return [0, 1];
  const low = Math.min(...data.map((candle) => candle.l));
  const high = Math.max(...data.map((candle) => candle.h));
  const padding = (high - low) * 0.05 || Math.abs(high) * 0.01 || 1;
  return [low - padding, high + padding];
}

function formatPrice(value: number): string {
  return value.toLocaleString(undefined, { maximumFractionDigits: 5 });
}

function formatAxisTime(value: number, timeframe: Timeframe): string {
  const date = new Date(value);
  if (timeframe === "1day") {
    return date.toLocaleDateString(undefined, { month: "short", day: "numeric" });
  }
  return date.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
}
