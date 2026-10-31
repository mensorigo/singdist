import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { SecretInput } from "../SecretInput";
import { randomHex } from "../helpers";

interface TrojanFieldsProps {
  tjPassword: string;
  setTjPassword: (v: string) => void;
  tjSni: string;
  setTjSni: (v: string) => void;
}

export function TrojanFields({
  tjPassword,
  setTjPassword,
  tjSni,
  setTjSni,
}: TrojanFieldsProps) {
  return (
            <>
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label htmlFor="inb-tjpw">访问密码</Label>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => setTjPassword(randomHex(8))}
                    className="h-7 rounded-lg px-2 text-xs"
                  >
                    随机生成
                  </Button>
                </div>
                <SecretInput
                  id="inb-tjpw"
                  value={tjPassword}
                  onChange={setTjPassword}
                  placeholder="留空则创建时自动生成"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="inb-tjsni">SNI（可选）</Label>
                <Input
                  id="inb-tjsni"
                  value={tjSni}
                  onChange={(e) => setTjSni(e.target.value)}
                  placeholder="证书对应的域名，可留空"
                />
              </div>
              <p className="text-xs text-muted-foreground">
                服务器证书将使用「证书管理」中的当前证书（同步阶段生效）。
              </p>
            </>
  );
}
