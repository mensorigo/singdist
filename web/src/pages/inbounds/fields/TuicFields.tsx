import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { SecretInput } from "../SecretInput";
import { randomHex } from "../helpers";

interface TuicFieldsProps {
  tuicUuid: string;
  setTuicUuid: (v: string) => void;
  tuicPassword: string;
  setTuicPassword: (v: string) => void;
  tuicCongestion: string;
  setTuicCongestion: (v: string) => void;
  tuicCongestionCustom: { value: string; label: string } | null;
  tuicSni: string;
  setTuicSni: (v: string) => void;
}

export function TuicFields({
  tuicUuid,
  setTuicUuid,
  tuicPassword,
  setTuicPassword,
  tuicCongestion,
  setTuicCongestion,
  tuicCongestionCustom,
  tuicSni,
  setTuicSni,
}: TuicFieldsProps) {
  return (
            <>
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label htmlFor="inb-tuuid">UUID</Label>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => setTuicUuid(crypto.randomUUID())}
                    className="h-7 rounded-lg px-2 text-xs"
                  >
                    随机生成
                  </Button>
                </div>
                <Input
                  id="inb-tuuid"
                  value={tuicUuid}
                  onChange={(e) => setTuicUuid(e.target.value)}
                  placeholder="留空则创建时自动生成"
                  className="font-mono text-xs"
                />
              </div>
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label htmlFor="inb-tpw">访问密码</Label>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => setTuicPassword(randomHex(8))}
                    className="h-7 rounded-lg px-2 text-xs"
                  >
                    随机生成
                  </Button>
                </div>
                <SecretInput
                  id="inb-tpw"
                  value={tuicPassword}
                  onChange={setTuicPassword}
                  placeholder="留空则创建时自动生成"
                />
              </div>
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="inb-tcc">拥塞控制</Label>
                  <Select
                    value={tuicCongestion}
                    onValueChange={setTuicCongestion}
                  >
                    <SelectTrigger id="inb-tcc">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="bbr">BBR（推荐）</SelectItem>
                      <SelectItem value="cubic">Cubic</SelectItem>
                      <SelectItem value="new_reno">New Reno</SelectItem>
                      {/* Temporary option, same as obfs above. */}
                      {tuicCongestionCustom && (
                        <SelectItem value={tuicCongestionCustom.value}>
                          {tuicCongestionCustom.label}
                        </SelectItem>
                      )}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="inb-tsni">SNI（可选）</Label>
                  <Input
                    id="inb-tsni"
                    value={tuicSni}
                    onChange={(e) => setTuicSni(e.target.value)}
                    placeholder="证书对应的域名，可留空"
                  />
                </div>
              </div>
              <p className="text-xs text-muted-foreground">
                服务器证书将使用「证书管理」中的当前证书（同步阶段生效）。
              </p>
            </>
  );
}
