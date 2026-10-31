import { useEffect, useState } from "react";
import QRCode from "qrcode";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import type { SubscriptionView } from "@/lib/api";
import i18n from "@/lib/i18n";

export function QrDialog({
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
              alt={i18n.t('subscriptions.k029')}
              className="h-[240px] w-[240px] rounded-lg border"
            />
          ) : (
            <p className="py-10 text-sm text-muted-foreground">
              {i18n.t('subscriptions.k085')}
            </p>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
