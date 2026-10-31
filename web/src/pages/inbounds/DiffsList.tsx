import type { InboundDiff } from "@/lib/api";
import { DIFF_FIELD_LABEL } from "./constants";

function shortVal(field: InboundDiff["field"], v: string): string {
  if (field === "publicKey" && v.length > 14) return `${v.slice(0, 10)}…`;
  return v === "" ? "（空）" : v;
}

// DiffsList renders the field-level panel-vs-running differences.

export function DiffsList({ diffs }: { diffs: InboundDiff[] }) {
  return (
    <ul className="mt-1.5 space-y-0.5 text-[11px] leading-snug text-amber-700 dark:text-amber-300">
      {diffs.map((d) => (
        <li key={d.field} className="font-mono">
          {DIFF_FIELD_LABEL[d.field]}：面板 {shortVal(d.field, d.panel)} ·
          运行 {shortVal(d.field, d.running)}
        </li>
      ))}
    </ul>
  );
}

// SecretInput is a mono input masked by default, with a show/hide toggle.
