import { Construction } from "lucide-react";

import { Card, CardContent } from "@/components/ui/card";

interface Props {
  title: string;
}

export default function PlaceholderPage({ title }: Props) {
  return (
    <div className="space-y-4">
      <Card>
        <CardContent className="flex flex-col items-center justify-center gap-3 py-16 text-center">
          <Construction className="h-10 w-10 text-muted-foreground" />
          <p className="text-lg font-medium">开发中</p>
          <p className="max-w-sm text-sm text-muted-foreground">
            「{title}」将在后续里程碑中实现。本里程碑包含登录、仪表盘与侧边栏。
          </p>
        </CardContent>
      </Card>
    </div>
  );
}
