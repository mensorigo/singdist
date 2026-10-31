import { Copy, Ellipsis, Link2, Pencil, Power, QrCode, RefreshCw, Trash } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import type { SubscriptionView } from "@/lib/api";

export const ICON_BTN_CLS =
  "flex h-8 w-8 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-muted hover:text-foreground";

// Lifecycle pill only (user decision 2026-10-25: no online-status pill;
// subscription enabled state is the only status shown).

export function RowActions({
  v,
  onCopyLink,
  onQr,
  onEdit,
  onResetLink,
  onResetAll,
  onToggleEnabled,
  onDelete,
}: {
  v: SubscriptionView;
  onCopyLink: () => void;
  onQr: () => void;
  onEdit: () => void;
  onResetLink: () => void;
  onResetAll: () => void;
  onToggleEnabled: () => void;
  onDelete: () => void;
}) {
  return (
    <div className="flex items-center gap-1">
      <button
        type="button"
        title="复制订阅链接"
        aria-label="复制订阅链接"
        onClick={onCopyLink}
        className={ICON_BTN_CLS}
      >
        <Copy className="h-4 w-4" />
      </button>
      <button
        type="button"
        title="二维码"
        aria-label="二维码"
        onClick={onQr}
        className={ICON_BTN_CLS}
      >
        <QrCode className="h-4 w-4" />
      </button>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            title="更多操作"
            aria-label="更多操作"
            className={ICON_BTN_CLS}
          >
            <Ellipsis className="h-4 w-4" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-32">
          <DropdownMenuItem className="text-[13px]" onClick={onEdit}>
            <Pencil className="mr-1.5 h-4 w-4" />
            编辑
          </DropdownMenuItem>
          <DropdownMenuItem className="text-[13px]" onClick={onResetLink}>
            <Link2 className="mr-1.5 h-4 w-4" />
            更换链接
          </DropdownMenuItem>
          <DropdownMenuItem className="text-[13px]" onClick={onResetAll}>
            <RefreshCw className="mr-1.5 h-4 w-4" />
            重置链接
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem className="text-[13px]" onClick={onToggleEnabled}>
            <Power className="mr-1.5 h-4 w-4" />
            {v.enabled ? "停用" : "启用"}
          </DropdownMenuItem>
          <DropdownMenuItem
            className="text-[13px]"
            onClick={onDelete}
          >
            <Trash className="mr-1.5 h-4 w-4" />
            删除
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

// Traffic bar + text, shared by both layouts: the percentage math lives
// here, so the templates only arrange pieces. barClassName is layout
// (table: fixed narrow bar; card: bar grows to fill the row).
