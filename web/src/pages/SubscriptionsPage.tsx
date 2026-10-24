import { useCallback, useEffect, useState } from "react";
import QRCode from "qrcode";
import {
  Check,
  Copy,
  Ellipsis,
  Eye,
  FingerprintPattern,
  Link2,
  Network,
  Pencil,
  Power,
  QrCode,
  Trash,
} from "lucide-react";
import { toast } from "sonner";

import { ConfirmDialog } from "@/components/confirm-dialog";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  api,
  ApiError,
  type SubscriptionView,
  type TemplateView,
} from "@/lib/api";
import { cn } from "@/lib/utils";

const ICON_BTN_CLS =
  "flex h-8 w-8 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-muted hover:text-foreground";

// Lifecycle pill + online pill, old-panel style. The online pill is a
// placeholder until the realtime-connections phase feeds it.
function StatusPills({ v }: { v: SubscriptionView }) {
  const life = {
    active: ["启用中", "text-emerald-600 dark:text-emerald-400", "bg-emerald-500"],
    disabled: ["已停用", "text-muted-foreground", "bg-muted-foreground"],
    expired: ["已过期", "text-destructive", "bg-destructive"],
    over_quota: ["已超额", "text-destructive", "bg-destructive"],
  }[v.status] as [string, string, string];
  return (
    <div className="flex items-center gap-1.5">
      <span
        className={cn(
          "inline-flex items-center gap-1.5 rounded-full bg-muted px-2 py-0.5 text-xs font-medium",
          life[1],
        )}
      >
        <span className={cn("h-1.5 w-1.5 rounded-full", life[2])} />
        {life[0]}
      </span>
      <span className="inline-flex items-center gap-1.5 rounded-full bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground">
        <span className="h-1.5 w-1.5 rounded-full bg-muted-foreground" />
        离线
      </span>
    </div>
  );
}

function fmtDate(unix: number): string {
  if (unix === 0) return "永不过期";
  const d = new Date(unix * 1000);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}

// Old-panel byte style: always two decimals ("1.74 GB", "1.00 TB").
function fmtBytes2(bytes: number): string {
  if (!bytes || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB", "PB"];
  const i = Math.min(
    Math.floor(Math.log(bytes) / Math.log(1024)),
    units.length - 1,
  );
  return `${(bytes / 1024 ** i).toFixed(2)} ${units[i]}`;
}

function unixToDateInput(unix: number): string {
  if (unix === 0) return "";
  const d = new Date(unix * 1000);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}

function dateInputToUnix(v: string): number {
  if (!v) return 0;
  const t = new Date(`${v}T23:59:59`).getTime();
  return Number.isNaN(t) ? 0 : Math.floor(t / 1000);
}

const UNITS = [
  { key: "MB", factor: 1024 ** 2 },
  { key: "GB", factor: 1024 ** 3 },
  { key: "TB", factor: 1024 ** 4 },
] as const;
type UnitKey = (typeof UNITS)[number]["key"];

function bytesToInput(bytes: number): { value: number; unit: UnitKey } {
  if (bytes === 0) return { value: 0, unit: "GB" };
  for (const u of [...UNITS].reverse()) {
    if (bytes % u.factor === 0) return { value: bytes / u.factor, unit: u.key };
  }
  const gb = UNITS[1];
  return { value: Math.round((bytes / gb.factor) * 100) / 100, unit: "GB" };
}

async function copyText(text: string, okMsg: string) {
  try {
    await navigator.clipboard.writeText(text);
    toast.success(okMsg);
  } catch {
    toast.error("复制失败，请手动复制");
  }
}

export default function SubscriptionsPage({
  refreshSignal,
}: {
  refreshSignal: number;
}) {
  const [subs, setSubs] = useState<SubscriptionView[] | null>(null);
  const [templates, setTemplates] = useState<TemplateView[]>([]);
  const [loadError, setLoadError] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  const [subDialog, setSubDialog] = useState<SubscriptionView | "new" | null>(
    null,
  );
  const [qrSub, setQrSub] = useState<SubscriptionView | null>(null);
  const [pendingDeleteSub, setPendingDeleteSub] =
    useState<SubscriptionView | null>(null);
  const [pendingResetUuid, setPendingResetUuid] =
    useState<SubscriptionView | null>(null);
  const [pendingResetLink, setPendingResetLink] =
    useState<SubscriptionView | null>(null);

  const refresh = useCallback(async () => {
    try {
      const [s, t] = await Promise.all([
        api.subscriptions.list(),
        api.templates.list(),
      ]);
      setSubs(s);
      setTemplates(t);
      setLoadError(false);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        window.location.reload();
        return;
      }
      setLoadError(true);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh, refreshSignal]);

  const runAction = async (
    id: number,
    fn: () => Promise<unknown>,
    okMsg: string,
  ) => {
    void id;
    setActionError(null);
    try {
      await fn();
      await refresh();
      toast.success(okMsg);
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : "操作失败，请重试");
    }
  };

  if (!subs) {
    return (
      <p className="text-sm text-muted-foreground">
        {loadError ? "暂时无法获取订阅数据，请稍后刷新重试" : "数据加载中…"}
      </p>
    );
  }

  const subLink = (v: SubscriptionView) =>
    `${window.location.origin}/sub/${v.token}`;

  return (
    <div className="space-y-4">
      <section className="rounded-xl border bg-card px-6 py-6 text-card-foreground shadow-sm">
        <div className="flex items-center justify-between">
          <p className="text-base font-semibold">订阅列表</p>
          <Button
            onClick={() => setSubDialog("new")}
            className="h-8 rounded-xl px-3"
          >
            新建订阅
          </Button>
        </div>
        {actionError && (
          <p className="mt-2 text-[13px] text-destructive">{actionError}</p>
        )}
        <div className="mt-3 overflow-x-auto">
          <table className="w-full min-w-[860px] text-sm">
            <thead>
              <tr className="border-b text-left text-xs text-muted-foreground">
                <th className="py-2 pr-3 font-medium">订阅</th>
                <th className="py-2 pr-3 font-medium">状态</th>
                <th className="py-2 pr-3 font-medium">流量</th>
                <th className="py-2 pr-3 font-medium">到期</th>
                <th className="py-2 pr-3 font-medium">UUID</th>
                <th className="py-2 text-right font-medium">操作</th>
              </tr>
            </thead>
            <tbody>
              {subs.map((v) => {
                const pct =
                  v.limitBytes > 0
                    ? Math.min(100, (v.usedBytes / v.limitBytes) * 100)
                    : 0;
                return (
                  <tr key={v.id} className="border-b last:border-0">
                    <td
                      className={cn(
                        "py-2.5 pr-3 font-medium",
                        !v.enabled && "text-muted-foreground",
                      )}
                    >
                      {v.name}
                    </td>
                    <td className="py-2.5 pr-3">
                      <StatusPills v={v} />
                    </td>
                    <td className="py-2.5 pr-3">
                      <div className="flex items-center gap-3">
                        <div className="h-1.5 w-28 shrink-0 overflow-hidden rounded-full bg-muted">
                          {v.limitBytes > 0 && pct > 0 && (
                            <div
                              className="h-full rounded-full bg-foreground/50"
                              style={{ width: `${pct}%` }}
                            />
                          )}
                        </div>
                        <span className="whitespace-nowrap tabular-nums text-[13px] text-muted-foreground">
                          {fmtBytes2(v.usedBytes)} /{" "}
                          {v.limitBytes > 0
                            ? `${fmtBytes2(v.limitBytes)} · ${pct.toFixed(1)}%`
                            : "不限"}
                        </span>
                      </div>
                    </td>
                    <td
                      className={cn(
                        "py-2.5 pr-3 tabular-nums",
                        v.expireAt > 0 ? "font-medium" : "text-muted-foreground",
                      )}
                    >
                      {fmtDate(v.expireAt)}
                    </td>
                    <td className="py-2.5 pr-3">
                      <button
                        type="button"
                        title={`${v.uuid}（点击复制）`}
                        onClick={() => void copyText(v.uuid, "UUID 已复制")}
                        className="font-mono text-xs text-muted-foreground transition-colors hover:text-foreground"
                      >
                        {v.uuid.slice(0, 19)}…
                      </button>
                    </td>
                    <td className="py-2.5">
                      <div className="flex items-center justify-end gap-1">
                        <button
                          type="button"
                          title="复制订阅链接"
                          aria-label="复制订阅链接"
                          onClick={() =>
                            void copyText(subLink(v), "订阅链接已复制")
                          }
                          className={ICON_BTN_CLS}
                        >
                          <Copy className="h-4 w-4" />
                        </button>
                        <button
                          type="button"
                          title="二维码"
                          aria-label="二维码"
                          onClick={() => setQrSub(v)}
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
                            <DropdownMenuItem
                              className="text-[13px]"
                              onClick={() => setSubDialog(v)}
                            >
                              <Pencil className="mr-1.5 h-4 w-4" />
                              编辑
                            </DropdownMenuItem>
                            <DropdownMenuItem disabled className="text-[13px]">
                              <Network className="mr-1.5 h-4 w-4" />
                              IP 记录
                            </DropdownMenuItem>
                            <DropdownMenuItem
                              className="text-[13px]"
                              onClick={() => setPendingResetLink(v)}
                            >
                              <Link2 className="mr-1.5 h-4 w-4" />
                              转换链接
                            </DropdownMenuItem>
                            <DropdownMenuItem
                              className="text-[13px]"
                              onClick={() => setPendingResetUuid(v)}
                            >
                              <FingerprintPattern className="mr-1.5 h-4 w-4" />
                              转换UUID
                            </DropdownMenuItem>
                            <DropdownMenuSeparator />
                            <DropdownMenuItem
                              className="text-[13px]"
                              onClick={() =>
                                void runAction(
                                  v.id,
                                  () =>
                                    api.subscriptions.setEnabled(
                                      v.id,
                                      !v.enabled,
                                    ),
                                  v.enabled ? "订阅已停用" : "订阅已启用",
                                )
                              }
                            >
                              <Power className="mr-1.5 h-4 w-4" />
                              {v.enabled ? "停用" : "启用"}
                            </DropdownMenuItem>
                            <DropdownMenuItem
                              className="text-[13px] text-destructive focus:text-destructive"
                              onClick={() => setPendingDeleteSub(v)}
                            >
                              <Trash className="mr-1.5 h-4 w-4" />
                              删除
                            </DropdownMenuItem>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </div>
                    </td>
                  </tr>
                );
              })}
              {subs.length === 0 && (
                <tr>
                  <td
                    colSpan={6}
                    className="py-6 text-center text-sm text-muted-foreground"
                  >
                    暂无订阅，点右上角「新建订阅」创建
                    {templates.length === 0 && "（需要先在「入站 → 模板」中新建模板）"}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </section>

      {subDialog !== null && (
        <SubscriptionFormDialog
          item={subDialog === "new" ? null : subDialog}
          templates={templates}
          onClose={() => setSubDialog(null)}
          onSaved={async () => {
            setSubDialog(null);
            await refresh();
          }}
        />
      )}
      {qrSub && <QrDialog sub={qrSub} onClose={() => setQrSub(null)} />}

      <ConfirmDialog
        open={pendingDeleteSub !== null}
        onOpenChange={(open) => {
          if (!open) setPendingDeleteSub(null);
        }}
        title="删除订阅"
        description={
          pendingDeleteSub
            ? `确定要删除订阅「${pendingDeleteSub.name}」吗？删除后该订阅的链接将不可用。`
            : undefined
        }
        confirmLabel="删除"
        destructive
        onConfirm={() => {
          if (!pendingDeleteSub) return;
          const v = pendingDeleteSub;
          setPendingDeleteSub(null);
          void runAction(v.id, () => api.subscriptions.remove(v.id), "订阅已删除");
        }}
      />
      <ConfirmDialog
        open={pendingResetLink !== null}
        onOpenChange={(open) => {
          if (!open) setPendingResetLink(null);
        }}
        title="转换订阅链接"
        description={
          pendingResetLink
            ? `确定要转换订阅「${pendingResetLink.name}」的链接吗？旧链接将立即失效，所有已导入的客户端需要换用新链接。`
            : ""
        }
        confirmLabel="转换"
        destructive
        onConfirm={() => {
          if (!pendingResetLink) return;
          const v = pendingResetLink;
          setPendingResetLink(null);
          void runAction(
            v.id,
            () => api.subscriptions.resetToken(v.id),
            "链接已转换，旧链接已失效",
          );
        }}
      />
      <ConfirmDialog
        open={pendingResetUuid !== null}
        onOpenChange={(open) => {
          if (!open) setPendingResetUuid(null);
        }}
        title="转换 UUID"
        description={
          pendingResetUuid
            ? `确定要为订阅「${pendingResetUuid.name}」生成一个新的 UUID 吗？旧 UUID 将失效；同步到核心后，使用旧配置的客户端将无法连接，需要重新导入订阅。`
            : undefined
        }
        confirmLabel="转换"
        destructive
        onConfirm={() => {
          if (!pendingResetUuid) return;
          const v = pendingResetUuid;
          setPendingResetUuid(null);
          void runAction(
            v.id,
            () => api.subscriptions.resetUuid(v.id),
            "UUID 已转换",
          );
        }}
      />
    </div>
  );
}

// --- subscription form ------------------------------------------------------

function SubscriptionFormDialog({
  item,
  templates,
  onClose,
  onSaved,
}: {
  item: SubscriptionView | null;
  templates: TemplateView[];
  onClose: () => void;
  onSaved: () => Promise<void>;
}) {
  const creating = item === null;
  const [name, setName] = useState(item?.name ?? "");
  const [templateId, setTemplateId] = useState<number>(
    item?.templateId ?? templates[0]?.id ?? 0,
  );
  const initLimit = bytesToInput(item?.limitBytes ?? 0);
  const [limitValue, setLimitValue] = useState(initLimit.value);
  const [limitUnit, setLimitUnit] = useState<UnitKey>(initLimit.unit);
  const [expireDate, setExpireDate] = useState(
    unixToDateInput(item?.expireAt ?? 0),
  );
  const [maxIps, setMaxIps] = useState(item?.maxIps ?? 0);
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [previewOpen, setPreviewOpen] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setFormError(null);
    if (!templateId) {
      setFormError("请先新建一个模板再创建订阅");
      return;
    }
    setSubmitting(true);
    try {
      const factor = UNITS.find((u) => u.key === limitUnit)!.factor;
      const body = {
        name: name.trim(),
        templateId,
        limitBytes: Math.round(limitValue * factor),
        expireAt: dateInputToUnix(expireDate),
        maxIps,
      };
      if (creating) {
        await api.subscriptions.create(body);
        toast.success("订阅已创建");
      } else {
        await api.subscriptions.update(item.id, body);
        toast.success("订阅已保存");
      }
      await onSaved();
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : "保存失败，请重试");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{creating ? "新建订阅" : "编辑订阅"}</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="sub-name">名称</Label>
            <Input
              id="sub-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="例如：Kavorys"
              required
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="sub-tpl">绑定模板</Label>
            <Select
              value={String(templateId)}
              onValueChange={(v) => setTemplateId(Number(v))}
            >
              <SelectTrigger id="sub-tpl">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {templates.length === 0 && (
                  <SelectItem value="0">（暂无模板）</SelectItem>
                )}
                {templates.map((t) => (
                  <SelectItem key={t.id} value={String(t.id)}>
                    {t.name}（{t.inboundIds.length} 个入站）
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="sub-limit">流量上限（0 = 不限）</Label>
              <div className="flex gap-2">
                <Input
                  id="sub-limit"
                  type="number"
                  min={0}
                  step="any"
                  value={limitValue}
                  onChange={(e) => setLimitValue(Number(e.target.value))}
                />
                <Select
                  value={limitUnit}
                  onValueChange={(v) => setLimitUnit(v as UnitKey)}
                >
                  <SelectTrigger
                    aria-label="流量单位"
                    className="w-20 shrink-0 px-2"
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {UNITS.map((u) => (
                      <SelectItem key={u.key} value={u.key}>
                        {u.key}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
            <div className="space-y-2">
              <Label htmlFor="sub-expire">到期日期（留空 = 永不过期）</Label>
              <Input
                id="sub-expire"
                type="date"
                value={expireDate}
                onChange={(e) => setExpireDate(e.target.value)}
              />
            </div>
          </div>
          <div className="space-y-2">
            <Label htmlFor="sub-maxips">同时使用 IP 上限（0 = 不限）</Label>
            <Input
              id="sub-maxips"
              type="number"
              min={0}
              value={maxIps}
              onChange={(e) => setMaxIps(Number(e.target.value))}
              className="w-36"
            />
            <p className="text-xs text-muted-foreground">
              多人共用一个订阅；IP 检测上线后生效。
            </p>
          </div>
          {!creating && item && (
            <div className="space-y-2">
              <Label>订阅链接</Label>
              <div className="flex gap-2">
                <Input
                  value={`${window.location.origin}/sub/${item.token}`}
                  readOnly
                  className="font-mono text-xs"
                />
                <Button
                  type="button"
                  variant="outline"
                  onClick={() =>
                    void copyText(
                      `${window.location.origin}/sub/${item.token}`,
                      "订阅链接已复制",
                    )
                  }
                  className="h-9 shrink-0 rounded-xl px-3"
                >
                  <Copy className="h-4 w-4" />
                  复制
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => setPreviewOpen(true)}
                  className="h-9 shrink-0 rounded-xl px-3"
                >
                  <Eye className="h-4 w-4" />
                  预览
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">
                更换链接：列表「···」菜单 → 转换链接。
              </p>
            </div>
          )}
          {!creating && item && (
            <div className="space-y-2">
              <Label>UUID（核心用户标识）</Label>
              <div className="flex gap-2">
                <Input
                  value={item.uuid}
                  readOnly
                  className="font-mono text-xs"
                />
                <Button
                  type="button"
                  variant="outline"
                  onClick={async () => {
                    try {
                      await navigator.clipboard.writeText(item.uuid);
                      setCopied(true);
                      toast.success("UUID 已复制");
                      setTimeout(() => setCopied(false), 1500);
                    } catch {
                      setFormError("复制失败，请手动复制");
                    }
                  }}
                  className="h-9 shrink-0 rounded-xl px-3"
                >
                  {copied ? (
                    <Check className="h-4 w-4" />
                  ) : (
                    <Copy className="h-4 w-4" />
                  )}
                  复制
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">
                更换 UUID：列表「···」菜单 → 转换UUID。
              </p>
            </div>
          )}
          {formError && (
            <p className="text-[13px] text-destructive">{formError}</p>
          )}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={onClose}
              disabled={submitting}
            >
              取消
            </Button>
            <Button type="submit" disabled={submitting}>
              {submitting ? "保存中…" : creating ? "创建" : "保存"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
      {!creating && item && previewOpen && (
        <PreviewDialog
          subId={item.id}
          name={item.name}
          onClose={() => setPreviewOpen(false)}
        />
      )}
    </Dialog>
  );
}

// --- subscription preview ---------------------------------------------------------

function PreviewDialog({
  subId,
  name,
  onClose,
}: {
  subId: number;
  name: string;
  onClose: () => void;
}) {
  const [content, setContent] = useState<string | null>(null);
  const [format, setFormat] = useState("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let live = true;
    api.subscriptions
      .preview(subId)
      .then((v) => {
        if (live) {
          setContent(v.content);
          setFormat(v.format);
        }
      })
      .catch((err) => {
        if (live) {
          setError(
            err instanceof ApiError ? err.message : "预览生成失败，请重试",
          );
        }
      });
    return () => {
      live = false;
    };
  }, [subId]);

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>
            订阅预览 · {name}
            {format && (
              <span className="ml-2 text-sm font-normal text-muted-foreground">
                {format === "clash" ? "Clash" : "sing-box JSON"}
              </span>
            )}
          </DialogTitle>
        </DialogHeader>
        {error ? (
          <p className="text-sm text-destructive">{error}</p>
        ) : content === null ? (
          <p className="text-sm text-muted-foreground">生成中…</p>
        ) : (
          <>
            <pre className="max-h-[55vh] overflow-auto rounded-lg border bg-muted/40 p-4 font-mono text-xs leading-relaxed">
              {content}
            </pre>
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => void copyText(content, "订阅内容已复制")}
              >
                <Copy className="h-4 w-4" />
                复制全文
              </Button>
              <Button type="button" onClick={onClose}>
                关闭
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}

// --- QR dialog ------------------------------------------------------------------

function QrDialog({
  sub,
  onClose,
}: {
  sub: SubscriptionView;
  onClose: () => void;
}) {
  const url = `${window.location.origin}/sub/${sub.token}`;
  const [qr, setQr] = useState<string | null>(null);

  useEffect(() => {
    let live = true;
    QRCode.toDataURL(url, { width: 240, margin: 1 })
      .then((d) => {
        if (live) setQr(d);
      })
      .catch(() => setQr(null));
    return () => {
      live = false;
    };
  }, [url]);

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="max-w-xs">
        <DialogHeader>
          <DialogTitle>{sub.name}</DialogTitle>
        </DialogHeader>
        <div className="flex justify-center py-2">
          {qr ? (
            <img
              src={qr}
              alt="订阅链接二维码"
              className="h-[240px] w-[240px] rounded-lg border"
            />
          ) : (
            <p className="py-10 text-sm text-muted-foreground">
              二维码生成失败
            </p>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
