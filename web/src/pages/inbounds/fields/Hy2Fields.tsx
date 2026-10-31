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

interface Hy2FieldsProps {
  upMbps: number;
  setUpMbps: (v: number) => void;
  downMbps: number;
  setDownMbps: (v: number) => void;
  password: string;
  setPassword: (v: string) => void;
  obfs: string;
  setObfs: (v: string) => void;
  obfsPassword: string;
  setObfsPassword: (v: string) => void;
  obfsCustom: { value: string; label: string } | null;
}

export function Hy2Fields({
  upMbps,
  setUpMbps,
  downMbps,
  setDownMbps,
  password,
  setPassword,
  obfs,
  setObfs,
  obfsPassword,
  setObfsPassword,
  obfsCustom,
}: Hy2FieldsProps) {
  return (
            <>
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="inb-up">上行 Mbps（0 = 不限）</Label>
                  <Input
                    id="inb-up"
                    type="number"
                    min={0}
                    value={upMbps}
                    onChange={(e) => setUpMbps(Number(e.target.value))}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="inb-down">下行 Mbps（0 = 不限）</Label>
                  <Input
                    id="inb-down"
                    type="number"
                    min={0}
                    value={downMbps}
                    onChange={(e) => setDownMbps(Number(e.target.value))}
                  />
                </div>
              </div>
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label htmlFor="inb-pw">访问密码</Label>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => setPassword(randomHex(8))}
                    className="h-7 rounded-lg px-2 text-xs"
                  >
                    随机生成
                  </Button>
                </div>
                <SecretInput
                  id="inb-pw"
                  value={password}
                  onChange={setPassword}
                  placeholder="留空则创建时自动生成"
                />
              </div>
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="inb-obfs">混淆</Label>
                  <Select
                    value={obfs === "" ? "none" : obfs}
                    onValueChange={(v) => setObfs(v === "none" ? "" : v)}
                  >
                    <SelectTrigger id="inb-obfs">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="none">无</SelectItem>
                      <SelectItem value="salamander">salamander</SelectItem>
                      {/* Temporary option for a value outside the fixed
                      list: shown while current, gone once a standard
                      option is picked. */}
                      {obfsCustom && (
                        <SelectItem value={obfsCustom.value}>
                          {obfsCustom.label}
                        </SelectItem>
                      )}
                    </SelectContent>
                  </Select>
                </div>
                {obfs === "salamander" && (
                  <div className="space-y-2">
                    <Label htmlFor="inb-obfspw">混淆密码</Label>
                    <SecretInput
                      id="inb-obfspw"
                      value={obfsPassword}
                      onChange={setObfsPassword}
                      placeholder="留空则自动生成"
                    />
                  </div>
                )}
              </div>
            </>
  );
}
