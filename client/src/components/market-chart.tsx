import {
  CandlestickSeries,
  createChart,
  type IChartApi,
  type ISeriesApi,
  type UTCTimestamp,
} from "lightweight-charts";
import { useEffect, useRef, useState } from "react";
import { intradayTimeframes, type Timeframe } from "../lib/market";

export type ChartCandle = {
  time: UTCTimestamp;
  open: number;
  high: number;
  low: number;
  close: number;
};

type Theme = "light" | "dark";

const upColor = "#087cec";
const downColor = "#e5484d";

const themeColors: Record<
  Theme,
  { text: string; border: string; grid: string; crosshair: string }
> = {
  light: { text: "#71717a", border: "#e4e4e7", grid: "#f0f0f2", crosshair: "#d4d4d8" },
  dark: { text: "#a1a1aa", border: "#27272a", grid: "#1e1e22", crosshair: "#3f3f46" },
};

function resolvedTheme(): Theme {
  return document.documentElement.classList.contains("dark") ? "dark" : "light";
}

function chartOptions(theme: Theme) {
  const colors = themeColors[theme];
  return {
    layout: {
      background: { color: "transparent" as const },
      textColor: colors.text,
      attributionLogo: false,
    },
    grid: {
      vertLines: { color: colors.grid },
      horzLines: { color: colors.grid },
    },
    rightPriceScale: { borderColor: colors.border },
    timeScale: { borderColor: colors.border },
    crosshair: {
      vertLine: { color: colors.crosshair, labelBackgroundColor: colors.border },
      horzLine: { color: colors.crosshair, labelBackgroundColor: colors.border },
    },
  };
}

export function MarketChart({
  candles,
  timeframe,
}: {
  candles: ChartCandle[];
  timeframe: string;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<IChartApi | null>(null);
  const seriesRef = useRef<ISeriesApi<"Candlestick"> | null>(null);
  const [hovered, setHovered] = useState<ChartCandle | null>(null);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    const chart = createChart(container, { ...chartOptions(resolvedTheme()), autoSize: true });
    const series = chart.addSeries(CandlestickSeries, {
      upColor,
      downColor,
      borderUpColor: upColor,
      borderDownColor: downColor,
      wickUpColor: upColor,
      wickDownColor: downColor,
    });
    chart.subscribeCrosshairMove((param) => {
      if (!param.time) {
        setHovered(null);
        return;
      }
      const data = param.seriesData.get(series) as ChartCandle | undefined;
      setHovered(data ?? null);
    });
    chartRef.current = chart;
    seriesRef.current = series;

    const observer = new MutationObserver(() => {
      chart.applyOptions(chartOptions(resolvedTheme()));
    });
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class"],
    });

    return () => {
      observer.disconnect();
      chart.remove();
      chartRef.current = null;
      seriesRef.current = null;
    };
  }, []);

  useEffect(() => {
    const chart = chartRef.current;
    if (!chart) return;
    // Intraday charts show clock times on the bottom axis; daily and longer
    // show dates only.
    const intraday = intradayTimeframes.has(timeframe as Timeframe);
    chart.applyOptions({ timeScale: { timeVisible: intraday, secondsVisible: false } });
  }, [timeframe]);

  useEffect(() => {
    const series = seriesRef.current;
    if (!series) return;
    if (candles.length === 0) {
      series.setData([]);
      return;
    }
    series.setData(candles);
    chartRef.current?.timeScale().fitContent();
  }, [candles]);

  return (
    <div className="relative">
      <div ref={containerRef} className="h-[440px]" />
      {hovered ? (
        <div className="pointer-events-none absolute top-2 left-2 rounded-lg border border-border bg-card px-3 py-2 text-xs shadow-sm">
          <p className="font-medium">{formatHoverTime(hovered.time, timeframe)}</p>
          <p className="mt-1 text-muted-foreground">
            O {formatPrice(hovered.open)} · H {formatPrice(hovered.high)} · L{" "}
            {formatPrice(hovered.low)} · C {formatPrice(hovered.close)}
          </p>
        </div>
      ) : null}
    </div>
  );
}

function formatPrice(value: number): string {
  return value.toLocaleString(undefined, { maximumFractionDigits: 5 });
}

function formatHoverTime(unixSeconds: UTCTimestamp, timeframe: string): string {
  const date = new Date(unixSeconds * 1000);
  if (!intradayTimeframes.has(timeframe as Timeframe)) {
    return date.toLocaleDateString(undefined, {
      month: "short",
      day: "numeric",
      year: "numeric",
    });
  }
  return date.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}
