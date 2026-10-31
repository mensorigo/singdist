import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { SecretInput } from "../SecretInput";
import { randomHex } from "../helpers";

interface AnyTlsFieldsProps {
  alPassword: string;
  setAlPassword: (v: string) => void;
  alSni: string;
  setAlSni: (v: string) => void;
}

export function AnyTlsFields({
  alPassword,
  setAlPassword,
  alSni,
  setAlSni,
}: AnyTlsFieldsProps) {
  return (
            <>
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label htmlFor="inb-alpw">访问密码</Label>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => setAlPassword(randomHex(8))}
                    className="h-7 rounded-lg px-2 text-xs"
                  >
                    随机生成
                  </Button>
                </div>
                <SecretInput
                  id="inb-alpw"
                  value={alPassword}
                  onChange={setAlPassword}
                  placeholder="留空则创建时自动生成"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="inb-alsni">SNI（可选）</Label>
                <Input
                  id="inb-alsni"
                  value={alSni}
                  onChange={(e) => setAlSni(e.target.value)}
                  placeholder="证书对应的域名，可留空"
                />
              </div>
              <p className="text-xs text-muted-foreground">
                服务器证书将使用「证书管理」中的当前证书（同步阶段生效）。
              </p>
            </>
  );
}
