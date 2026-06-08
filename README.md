# Kindle 墙面 Dashboard

把一台越狱的 **Kindle PW3** 当墙面显示器,常驻显示本机(Windows + WSL)的
**CPU / GPU 使用率、温度、内存**,带 btop 式镜像历史图。

服务端跑在 **WSL**:采集数据 → Pillow 画成 1072×1448 灰度图 → SSH 推到 Kindle 用
`eips` 刷屏。Kindle 只当"网络相框",不做任何渲染。

```
┌──────────────── WSL (服务端) ────────────────┐         ┌──────── Kindle PW3 ────────┐
│ 采集: CPU/温度/内存 ← LibreHardwareMonitor    │  SSH    │ 接收 PNG → /usr/sbin/eips   │
│       GPU ← nvidia-smi (硬件透传)             │ ──────▶ │ 局部刷新 (eips -x -y -w du) │
│ 画图: Pillow, 镜像 sweep 历史图               │ 推 PNG  │ (10.0.0.43)                 │
│ 推送: sweep 只刷新增列 + 变化的数字 (低损耗)  │         │                             │
└───────────────────────────────────────────────┘         └─────────────────────────────┘
```

## 界面

竖屏上下两块(**上 CPU / 下 GPU**),每块顶部三列大数字(使用率 / 温度 / 内存)+
下方一大块**上下镜像对称历史图**。每次更新只在图右侧加一条竖线,画满后清空从左重画——
这样每次只需局部刷新一小条,eink 损耗和延迟都最低。开机/关机各有一屏欢迎/告别。

## 文件结构

```
README.md          ← 本文件 (总览 + 部署)
config.sh          ← 机器相关配置 (换机器只改这个)
install.sh         ← 一键部署 (装依赖 / 配 SSH / 自检)
kindlectl          ← 服务管理 (enable/disable/start/stop/status/log)
kindle_dash.py     ← worker (采集 / 画图 / 推送循环)
docs/
  使用说明.md       ← 详细中文说明 (参数 / 刷新策略 / 故障排查)
  plan.md          ← 原始项目规划与后续方向
```

## 在新机器上部署

前提:Kindle 已越狱并配好 SSH(NiLuJe usbnet,WiFi 可达),WSL 能 `ssh` 到它。

```bash
# 1. 改配置 (发行版/用户名/Kindle IP 等)
nano config.sh

# 2. 一键部署 (装依赖 + 写 ssh config + 自检)
./install.sh

# 3. (可选, 为了 CPU 占用/温度/内存) 在 Windows 装并运行 LibreHardwareMonitor:
#    - 管理员运行常驻; Options → Remote Web Server (8085); 防火墙放行 8085 入站
#    GPU 数据走 nvidia-smi, 不依赖它

# 4. 开机自启 + 启动
./kindlectl enable
./kindlectl status
```

换机器要改的都集中在 **`config.sh`**:WSL 发行版名、WSL 用户、Windows 用户、
Kindle IP、刷新参数。字体(PingFang SC → 雅黑 → …)和 Windows 宿主机 IP(走 WSL 网关)
都自动发现,无需手填。

## 常用命令

```bash
./kindlectl enable | disable | start | stop | restart | status | log

# 调试 (前台直接看输出)
python3 kindle_dash.py --interval 10          # 前台常驻
python3 kindle_dash.py --once --save x.png     # 画一帧存本地
python3 kindle_dash.py --message "你好 Kindle" # 推一屏文字
```

详细参数、刷新策略、故障排查见 **[docs/使用说明.md](docs/使用说明.md)**。

## Go 版（新，跨平台，去 WSL）

仓库下 `go/` 是用 Go 重写的版本，**在 Windows / macOS 原生跑**，单个二进制，
不再需要 WSL / ControlMaster / VBS / nvidia-smi.exe。功能完全对齐（采集 + 渲染
+ SSH 推送 + 自启），Kindle 端协议不变。LibreHardwareMonitor 由「必需」降级为
「可选」——只为 CPU 包封温这一个指标，不填 `temp.lhm_url` → 0 外部依赖。

- 一键部署：
  - Windows：双击 [`scripts\install-windows.cmd`](scripts/install-windows.cmd)
  - macOS：`./scripts/install-macos.sh`
- 手动运行 / 调试 / 配置文件 / 子命令速查 / 自启原理 / 故障排查：
  全在 **[docs/部署-go版.md](docs/部署-go版.md)**
- 设计文档：[refactor.plan.md](refactor.plan.md)

## 为什么 CPU 数据走 LibreHardwareMonitor 而不是 psutil

WSL 里的 `psutil` 只能看到 **WSL 子系统**,看不到 Windows 整机——游戏跑在 Windows
侧时 CPU 会显示 ~0%。所以 CPU 占用/温度/内存都从 Windows 端的 LibreHardwareMonitor
读(整机视角),LHM 不可用时才降级回 psutil。GPU 走 nvidia-smi(硬件透传,本就是真实值)。
