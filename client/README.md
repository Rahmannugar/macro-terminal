# Macro Terminal — Client

Macro Terminal is a macroeconomic intelligence terminal that connects economic data, financial news, market data, and economic narratives into a unified information graph.

This directory contains the Macro Terminal web application: a React
single-page client built with Vite and served as static files.

## Technology

- React 19 and Vite
- TypeScript
- React Router
- Tailwind CSS 4
- Biome
- Bun 1.4
- ByteDatePicker
- Authrail
- Recharts

## Local Development

Install the Bun version declared in `package.json`, then install the locked
dependencies:

```bash
bun install --frozen-lockfile
```

Start the development server:

```bash
bun run dev
```

Open [http://localhost:5173](http://localhost:5173).

## Validation

Run formatting, lint, and TypeScript checks:

```bash
bun run check
```

Build the production application:

```bash
bun run build
```
