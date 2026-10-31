import { useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  api,
  ApiError,
  type ImportCandidate,
  type NodeView,
} from "@/lib/api";
import i18n from "@/lib/i18n";

export function NodeImportDialog({
  node,
  candidates,
  onClose,
  onImported,
}: {
  node: NodeView;
  candidates: ImportCandidate[];
  onClose: () => void;
  onImported: () => Promise<void>;
}) {
  const importable = candidates.filter((c) => !c.disabledReason);
  const [selected, setSelected] = useState<Set<string>>(
    () => new Set(importable.map((c) => c.name)),
  );
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  const toggle = (name: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(name)) {
        next.delete(name);
      } else {
        next.add(name);
      }
      return next;
    });
  };

  const handleConfirm = async () => {
    setSubmitting(true);
    setFormError(null);
    try {
      const res = await api.nodes.importInbounds(node.id, [...selected]);
      if (res.created.length > 0) {
        toast.success(
          i18n.t('nodes.k066', { count: res.created.length, names: res.created.map((c) => c.name).join("、") }),
        );
      }
      for (const s of res.skipped) {
        toast.warning(i18n.t('nodes.k067', { name: s.name, reason: s.reason }));
      }
      if (res.created.length === 0 && res.skipped.length === 0) {
        toast(i18n.t('nodes.k068'));
      }
      await onImported();
    } catch (err) {
      setFormError(
        err instanceof ApiError ? err.message : i18n.t('nodes.k069'),
      );
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
          <DialogTitle>{i18n.t('nodes.k089', { value: node.name })}</DialogTitle>
        </DialogHeader>
        <p className="text-sm text-muted-foreground">
          {i18n.t('nodes.k090')}
        </p>
        <div className="space-y-2">
          {candidates.map((c) => (
            <label key={c.name} className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={selected.has(c.name)}
                disabled={!!c.disabledReason}
                onChange={() => toggle(c.name)}
              />
              <span>{c.name}</span>
              <span className="text-muted-foreground">
                {c.protocol} · {c.port}
              </span>
              {c.disabledReason && (
                <span className="text-[13px] text-destructive">
                  {c.disabledReason}
                </span>
              )}
            </label>
          ))}
        </div>
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
            {i18n.t('nodes.k091')}
          </Button>
          <Button
            onClick={handleConfirm}
            disabled={submitting || selected.size === 0}
          >
            {submitting ? i18n.t('nodes.k070') : i18n.t('nodes.k071')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
