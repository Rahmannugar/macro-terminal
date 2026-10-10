type TabsProps<T extends string> = {
  value: T;
  options: readonly T[];
  onChange: (value: T) => void;
  label?: (value: T) => string;
  size?: "sm" | "md";
};

export function Tabs<T extends string>({
  value,
  options,
  onChange,
  label,
  size = "md",
}: TabsProps<T>) {
  const height = size === "sm" ? "h-9 px-4 text-sm" : "h-11 px-4 text-sm";
  return (
    <div className="inline-flex items-center gap-1 rounded-lg border border-border bg-card p-1">
      {options.map((option) => (
        <button
          key={option}
          type="button"
          aria-pressed={value === option}
          className={`rounded-md font-semibold transition-colors ${height} ${
            value === option
              ? "bg-primary text-white"
              : "text-muted-foreground hover:bg-secondary hover:text-foreground"
          }`}
          onClick={() => onChange(option)}
        >
          {label ? label(option) : option}
        </button>
      ))}
    </div>
  );
}
