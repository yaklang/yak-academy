#!/bin/sh
# AWD SSH 攻击盒入口：按控制面下发的 USER_NAME / USER_PASSWORD 建用户并拉起 sshd。
# 安全约束：只允许密码登录、禁止 root 登录、不允许空密码；不安装 sudo。
set -eu

user_name="${USER_NAME:-player}"
user_password="${USER_PASSWORD:-}"
if [ -z "$user_password" ]; then
  echo "awd-attackbox: USER_PASSWORD is required" >&2
  exit 1
fi
if ! printf '%s' "$user_name" | grep -Eq '^[a-z_][a-z0-9_-]*$'; then
  echo "awd-attackbox: invalid USER_NAME" >&2
  exit 1
fi

if ! id "$user_name" >/dev/null 2>&1; then
  adduser -D -s /bin/bash "$user_name"
fi
printf '%s:%s\n' "$user_name" "$user_password" | chpasswd

ssh_config=/etc/ssh/sshd_config
sed -i \
  -e 's/^#\?PermitRootLogin.*/PermitRootLogin no/' \
  -e 's/^#\?PasswordAuthentication.*/PasswordAuthentication yes/' \
  -e 's/^#\?PermitEmptyPasswords.*/PermitEmptyPasswords no/' \
  -e 's/^#\?PubkeyAuthentication.*/PubkeyAuthentication no/' \
  "$ssh_config"

mkdir -p /run/sshd
ssh-keygen -A >/dev/null
exec /usr/sbin/sshd -D -e
