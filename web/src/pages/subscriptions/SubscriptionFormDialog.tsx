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
import i18n from "@/lib/i18n";

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
      setFormError(i18n.t('subscriptions.k043'));
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
        toast.success(i18n.t('subscriptions.k044'));
      } else {
        await api.subscriptions.update(item.id, body);
        toast.success(i18n.t('subscriptions.k045'));
      }
      await onSaved();
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : i18n.t('subscriptions.k046'));
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
          <DialogTitle>{creating ? i18n.t('subscriptions.k047') : i18n.t('subscriptions.k048')}</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="sub-name">{i18n.t('subscriptions.k049')}</Label>
            <Input
              id="sub-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={i18n.t('subscriptions.k050')}
              required
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="sub-tpl">{i18n.t('subscriptions.k051')}</Label>
            <Select
              value={String(templateId)}
              onValueChange={(v) => setTemplateId(Number(v))}
            >
              <SelectTrigger id="sub-tpl">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {templates.length === 0 && (
                  <SelectItem value="0">{i18n.t('subscriptions.k052')}</SelectItem>
                )}
                {templates.map((t) => (
                  <SelectItem key={t.id} value={String(t.id)}>
                    {i18n.t('subscriptions.k092', { value: t.name, value2: t.inboundIds.length })}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="sub-limit">{i18n.t('subscriptions.k053')}</Label>
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
                    aria-label={i18n.t('subscriptions.k054')}
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
              <Label htmlFor="sub-expire">{i18n.t('subscriptions.k055')}</Label>
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
              <Label>{i18n.t('subscriptions.k056')}</Label>
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
                      i18n.t('subscriptions.k057'),
                    )
                  }
                  className="h-9 shrink-0 rounded-xl px-3"
                >
                  <Copy className="h-4 w-4" />
                  {i18n.t('subscriptions.k093')}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => setPreviewOpen(true)}
                  className="h-9 shrink-0 rounded-xl px-3"
                >
                  <Eye className="h-4 w-4" />
                  {i18n.t('subscriptions.k094')}
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">
                {i18n.t('subscriptions.k095')}
              </p>
            </div>
          )}
          {!creating && item && (
            <div className="space-y-2">
              <Label>{i18n.t('subscriptions.k058')}</Label>
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
                      toast.success(i18n.t('subscriptions.k059'));
                      setTimeout(() => setCopied(false), 1500);
                    } catch {
                      setFormError(i18n.t('subscriptions.k060'));
                    }
                  }}
                  className="h-9 shrink-0 rounded-xl px-3"
                >
                  {copied ? (
                    <Check className="h-4 w-4" />
                  ) : (
                    <Copy className="h-4 w-4" />
                  )}
                  {i18n.t('subscriptions.k096')}
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">
                {i18n.t('subscriptions.k097')}
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
              {i18n.t('subscriptions.k098')}
            </Button>
            <Button type="submit" disabled={submitting}>
              {submitting ? i18n.t('subscriptions.k061') : creating ? i18n.t('subscriptions.k062') : i18n.t('subscriptions.k063')}
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
