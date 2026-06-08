#!/usr/bin/env bash
# install.sh —— Kindle Dashboard 一键部署 (在新机器的 WSL 里跑一次)
#   1. 装 Python 依赖
#   2. 写 ~/.ssh/config 的 kindle 别名 (若缺)
#   3. 给脚本加可执行权限
#   4. 测试到 Kindle 的 SSH
#   5. 打印剩余手动步骤 (LHM / 防火墙 / 开机自启)
#
# 部署前先改 config.sh 里的机器相关配置!
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$HERE/config.sh"

echo "==> 配置: DISTRO=$DISTRO  WSL_USER=$WSL_USER  WIN_USER=$WIN_USER  KINDLE_IP=$KINDLE_IP"
echo "==> 项目目录: $PROJECT_DIR"
echo

echo "==> [1/4] 安装 Python 依赖 (psutil pynvml pillow)…"
pip install --break-system-packages -q psutil pynvml pillow
echo "    完成"
echo

echo "==> [2/4] 检查 ~/.ssh/config 的 kindle 别名…"
SSH_CONF="$HOME/.ssh/config"
mkdir -p "$HOME/.ssh"; chmod 700 "$HOME/.ssh"
if grep -qE '^Host[[:space:]]+kindle([[:space:]]|$)' "$SSH_CONF" 2>/dev/null; then
    echo "    已存在, 跳过"
else
    cat >> "$SSH_CONF" <<EOF

Host kindle
    HostName $KINDLE_IP
    User root
    ControlMaster auto
    ControlPath ~/.ssh/cm-%r@%h:%p
    ControlPersist 600
EOF
    chmod 600 "$SSH_CONF"
    echo "    已写入 (HostName=$KINDLE_IP)"
    echo "    注意: 首次连接需你手动配好免密 (ssh-copy-id 或越狱包里已配)"
fi
echo

echo "==> [3/4] 给脚本加可执行权限…"
chmod +x "$PROJECT_DIR/kindlectl" "$PROJECT_DIR/install.sh"
echo "    完成"
echo

echo "==> [4/4] 测试到 Kindle 的 SSH (5s 超时)…"
if timeout 8 ssh -o ConnectTimeout=5 kindle "ls /usr/sbin/eips" >/dev/null 2>&1; then
    echo "    OK: 能连上 Kindle 且 /usr/sbin/eips 存在"
else
    echo "    !! 连不上 Kindle —— 检查: Kindle 开机/同网段/WiFi、免密是否配好、IP 是否对"
fi
echo

cat <<'EOF'
======================== 剩余手动步骤 ========================
A. CPU 占用/温度/内存 需要 Windows 端的 LibreHardwareMonitor:
   1) 装 LibreHardwareMonitor (普通版即可), 以【管理员】运行并常驻
   2) Options → Remote Web Server → Run (端口 8085)
   3) 防火墙放行 8085 入站 (否则 WSL 连不到, 会降级走 powershell)
   - 不装也能跑, 只是 CPU 那几项会空 (GPU 不受影响)

B. 启动 / 开机自启:
   ./kindlectl enable     # 开机自启 + 立即启动
   ./kindlectl status     # 看状态
   ./kindlectl log        # 看日志
=============================================================
EOF
echo "部署脚本结束。"
