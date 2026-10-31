import { useEffect, useState } from "react";
import { Copy } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { api, ApiError } from "@/lib/api";
import { copyText } from "./format";
import i18n from "@/lib/i18n";

export function PreviewDialog({
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
            err instanceof ApiError ? err.message : i18n.t('subscriptions.k026'),
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
            {i18n.t('subscriptions.k082', { name: name })}
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
          <p className="text-sm text-muted-foreground">{i18n.t('subscriptions.k027')}</p>
        ) : (
          <>
            <pre className="max-h-[55vh] overflow-auto rounded-lg border bg-muted/40 p-4 font-mono text-xs leading-relaxed">
              {content}
            </pre>
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => void copyText(content, i18n.t('subscriptions.k028'))}
              >
                <Copy className="h-4 w-4" />
                {i18n.t('subscriptions.k083')}
              </Button>
              <Button type="button" onClick={onClose}>
                {i18n.t('subscriptions.k084')}
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
