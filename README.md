# Kindle 墙面 Dashboard

把一台越狱的 **Kindle PW3** 当墙面显示器,常驻显示本机的
**CPU / GPU 使用率、温度、内存**,带 btop 式镜像历史图。

服务端是一个用 Go 写的**单二进制**,在 **Windows / macOS** 原生跑：采集数据 →
画成 1072×1448 灰度 PNG → 通过进程内 SSH 长连接推到 Kindle 用 `eips` 刷屏。
Kindle 只当"网络相框",不做任何渲染。

```
┌──────────────── 宿主机 (Windows / macOS) ────────────────┐         ┌──────── Kindle PW3 ────────┐
│ 采集: CPU 占用/内存 ← 原生 API (gopsutil)                  │  SSH    │ 接收 PNG → /usr/sbin/eips   │
│       CPU 温度  ← PawnIO (AMD) / SMC (mac) / LHM (可选)    │ ──────▶ │ 局部刷新 (eips -x -y -w du) │
│       GPU       ← NVML / IOKit                            │ 推 PNG  │ (10.0.0.43)                 │
│ 画图: 镜像 sweep 历史图                                    │         │                             │
│ 推送: sweep 只刷新增列 + 变化的数字 (低损耗)               │         │                             │
└────────────────────────────────────────────────────────────┘         └─────────────────────────────┘
```

## 界面

竖屏上下两块(**上 CPU / 下 GPU**),每块顶部三列大数字(使用率 / 温度 / 内存)+
下方一大块**上下镜像对称历史图**。每次更新只在图右侧加一条竖线,画满后清空从左重画——
这样每次只需局部刷新一小条,eink 损耗和延迟都最低。开机/关机各有一屏欢迎/告别。

## 文件结构

```
README.md                  ← 本文件 (总览 + 部署)
THIRD-PARTY-NOTICES.md     ← 第三方组件声明
go/                        ← Go 实现 (kindle-dash 二进制源码)
scripts/                   ← 安装 / 卸载脚本 (Win + mac)
docs/部署-go版.md           ← 详细部署文档 (配置项 / 故障排查 / 自启原理)
```

## 安装

前提:Kindle 已越狱并配好 SSH(NiLuJe usbnet,WiFi 可达)。

**Windows**(需要 UAC 弹窗一次,注册 Task Scheduler 高权限任务):

```
scripts\install-windows.cmd
```

或在终端:

```powershell
powershell -ExecutionPolicy Bypass -File scripts\install-windows.ps1
```

**macOS**:

```bash
chmod +x scripts/install-macos.sh && ./scripts/install-macos.sh
```

安装脚本会自动 build 二进制(需要已装 Go)、拷到标准位置、注册登录自启。

安装后务必配置 Kindle IP(首次安装必做):

```powershell
# Windows
notepad $env:APPDATA\kindle-dash\config.json
```

```bash
# macOS
nano ~/.config/kindle-dash/config.json
```

配置文件至少填:

```json
{ "kindle": { "host": "10.0.0.43" } }
```

## 常用命令

```powershell
# Windows(二进制装在此路径)
& "$env:LOCALAPPDATA\Programs\kindle-dash\kindle-dash.exe" start

# macOS / 已加入 PATH 后
kindle-dash start
```

```powershell
kindle-dash stop        # 优雅停止 (会推告别屏)
kindle-dash restart
kindle-dash status      # 查看安装与运行状态
kindle-dash run         # 前台运行 (看实时输出, Ctrl-C 优雅退出)
kindle-dash doctor      # SSH + Kindle 可达性自检 (出问题第一步)
```

## 卸载

```powershell
# Windows
scripts\uninstall-windows.cmd

# macOS
./scripts/uninstall-macos.sh
```

详细配置项、故障排查、自启原理见 **[docs/部署-go版.md](docs/部署-go版.md)**。

## CPU 温度的依赖

- **AMD Ryzen (Windows)**: 内置 PawnIO 模块读 SMN 寄存器,无外部依赖。
- **Intel / 其他 (Windows)**: 可选 [LibreHardwareMonitor](https://github.com/LibreHardwareMonitor/LibreHardwareMonitor)
  开 Remote Web Server (8085),在配置里填 `temp.lhm_url`。不填就显示 0。
- **macOS**: 走 SMC,无外部依赖。
