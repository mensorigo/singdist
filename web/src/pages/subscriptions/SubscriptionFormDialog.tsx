import { useState } from "react";
import { Check, Copy, Eye } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
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
import {
  UNITS,
  bytesToInput,
  copyText,
  dateInputToUnix,
  unixToDateInput,
  type UnitKey,
} from "./format";
import { PreviewDialog } from "./PreviewDialog";

export function SubscriptionFormDialog({
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
        // IP cap removed from the form (feature dropped): keep whatever
        // the row already has on edit, 0 on create. The backend column
        // stays but was never enforced, so this changes nothing.
        maxIps: item?.maxIps ?? 0,
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
                更换链接：列表「···」菜单 → 更换链接。
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
                更换 UUID：列表「···」菜单 → 重置链接。
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
