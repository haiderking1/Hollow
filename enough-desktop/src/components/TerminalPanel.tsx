interface TerminalPanelProps {
  open: boolean;
}

export default function TerminalPanel({ open }: TerminalPanelProps) {
  return (
    <div
      className="flex shrink-0 flex-col overflow-hidden border-t border-border bg-background transition-all duration-200 ease-in-out"
      style={{
        height: open ? 180 : 0,
        opacity: open ? 1 : 0,
        borderTopWidth: open ? 1 : 0,
      }}
    >
      <div className="flex h-full shrink-0 flex-col" style={{ height: 180 }}>
        <div className="flex h-8 shrink-0 items-center gap-2.5 border-b border-border bg-sidebar px-3.5">
          <div className="flex items-center gap-1.5">
            <span className="h-2 w-2 rounded-full bg-success" />
            <span className="font-mono text-[11px] font-medium text-foreground/80">bash</span>
          </div>
          <span className="h-3.5 w-px bg-border" />
          <span className="font-mono text-[10px] text-muted-foreground">~/projects/flame-desktop</span>
        </div>
        <div className="flex-1 overflow-y-auto p-3.5 font-mono text-xs leading-relaxed text-foreground/80">
          <div className="flex items-center gap-1.5">
            <span className="text-success">➜</span>
            <span className="text-accent">flame-desktop</span>
            <span className="inline-block h-[1.05em] w-[7px] translate-y-[1px] bg-success animate-caret" />
          </div>
        </div>
      </div>
    </div>
  );
}
