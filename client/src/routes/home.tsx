export function HomeRoute() {
  return (
    <main className="mx-auto flex min-h-svh max-w-[1160px] flex-col justify-center px-5 py-8 sm:px-8">
      <img src="/macroterminal.png" alt="" width={72} height={72} className="rounded-xl" />
      <h1 className="mt-5 font-display text-[28px] font-semibold tracking-[-0.04em]">
        Macro Terminal
      </h1>
      <p className="mt-2 max-w-[560px] text-sm text-muted-foreground">
        Macro intelligence and fundamental research terminal.
      </p>
    </main>
  );
}
