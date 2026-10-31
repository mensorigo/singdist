#!/bin/bash

if [ "$(id -u)" -ne 0 ]; then
  echo "权限检查：失败，需要 root 运行"
  exit 1
fi
echo "权限检查：成功"

SSH_PORTS=""
if [ -n "${SSH_CONNECTION:-}" ]; then
  SSH_PORTS="$SSH_PORTS $(echo "$SSH_CONNECTION" | awk '{print $4}')"
fi
if command -v sshd >/dev/null 2>&1; then
  SSH_PORTS="$SSH_PORTS $(sshd -T 2>/dev/null | awk '/^port /{print $2}')"
fi
if command -v ss >/dev/null 2>&1; then
  SSH_PORTS="$SSH_PORTS $(ss -tlnp 2>/dev/null | awk '/sshd/ {split($4, a, ":"); print a[length(a)]}')"
fi
SSH_PORTS="$(echo "$SSH_PORTS" | tr ' ' '\n' | grep -E '^[0-9]+$' | sort -nu | tr '\n' ' ')"
if [ -z "$SSH_PORTS" ]; then
  echo "检测 SSH 端口：失败，未检测到端口"
  exit 1
fi
echo "检测 SSH 端口：成功（端口：$SSH_PORTS）"

EXTRA_PORTS="${EXTRA_PORTS:-}"
ALL_ALLOW_PORTS="$SSH_PORTS $EXTRA_PORTS"

export DEBIAN_FRONTEND=noninteractive
if apt-get update >/dev/null 2>&1 && apt-get -o Dpkg::Options::="--force-confdef" -o Dpkg::Options::="--force-confold" upgrade -y >/dev/null 2>&1 && apt-get -o Dpkg::Options::="--force-confdef" -o Dpkg::Options::="--force-confold" install -y ufw fail2ban unattended-upgrades >/dev/null 2>&1 && printf 'APT::Periodic::Update-Package-Lists "1";\nAPT::Periodic::Unattended-Upgrade "1";\n' > /etc/apt/apt.conf.d/20auto-upgrades; then
  echo "系统更新：成功"
else
  echo "系统更新：失败"
  exit 1
fi

for svc in ModemManager udisks2 multipathd packagekit; do
  systemctl disable --now "$svc" >/dev/null 2>&1 || true
done
systemctl mask --now fwupd >/dev/null 2>&1 || true
echo "关闭无用服务：成功"

UFW_OK=1
for p in $ALL_ALLOW_PORTS; do
  if ! echo "$p" | grep -Eq '^[0-9]+$'; then
    echo "防火墙：失败，端口无效：$p"
    exit 1
  fi
  if ! ufw status 2>/dev/null | grep -q "$p/tcp"; then
    ufw limit "$p/tcp" >/dev/null 2>&1 || UFW_OK=0
  fi
done
ufw default deny incoming >/dev/null 2>&1 || UFW_OK=0
ufw default allow outgoing >/dev/null 2>&1 || UFW_OK=0
ufw --force enable >/dev/null 2>&1 || UFW_OK=0
if [ "$UFW_OK" -eq 1 ] && ufw status 2>/dev/null | grep -q "Status: active"; then
  echo "防火墙：成功（端口：$SSH_PORTS，已启用）"
else
  echo "防火墙：失败"
  exit 1
fi

F2B_PORTS="$(echo "$SSH_PORTS" | tr ' ' ',')"
printf '[sshd]\nenabled = true\nport = %s\nmaxretry = 5\nfindtime = 600\nbantime = 3600\nbackend = systemd\n' "$F2B_PORTS" > /etc/fail2ban/jail.local
systemctl enable fail2ban >/dev/null 2>&1 || true
systemctl restart fail2ban >/dev/null 2>&1 || true
sleep 3
if ! fail2ban-client status sshd >/dev/null 2>&1; then
  FIRST_PORT="$(echo "$SSH_PORTS" | awk '{print $1}')"
  printf '[sshd]\nenabled = true\nport = %s\nmaxretry = 5\nfindtime = 600\nbantime = 3600\nbackend = systemd\n' "$FIRST_PORT" > /etc/fail2ban/jail.local
  systemctl restart fail2ban >/dev/null 2>&1 || true
  sleep 3
fi
if fail2ban-client status sshd >/dev/null 2>&1; then
  echo "防爆破：成功（5次/10分钟/封1小时）"
else
  echo "防爆破：失败"
  exit 1
fi

echo "全部完成：成功"
