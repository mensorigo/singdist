import { Construction } from "lucide-react";

import { Card, CardContent } from "@/components/ui/card";
import i18n from "@/lib/i18n";

interface Props {
  title: string;
}

export default function PlaceholderPage({ title }: Props) {
  return (
    <div className="space-y-4">
      <Card>
        <CardContent className="flex flex-col items-center justify-center gap-3 py-16 text-center">
          <Construction className="h-10 w-10 text-muted-foreground" />
          <p className="text-lg font-medium">{i18n.t('common.k014')}</p>
          <p className="max-w-sm text-sm text-muted-foreground">
            {i18n.t('common.k015', { title: title })}
          </p>
        </CardContent>
      </Card>
    </div>
  );
}
