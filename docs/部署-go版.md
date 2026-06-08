# 部署 (Go 版)

Go 重写的 `kindle-dash` 取代旧的 `kindle_dash.py` + `kindlectl` + `install.sh` +
`config.sh` 那一套。**不依赖 WSL、ControlMaster、VBS、nvidia-smi.exe**——
一个静态二进制干掉所有附带复杂度。Kindle 端协议不变。

**剩下的唯一可选依赖：LibreHardwareMonitor**——只在你想看 CPU 包封温的情况下需要。
Windows userspace 没有读 CPU 温度的原生 API（要 kernel 驱动读 MSR），所以
plan §6 走「opt-in 走 LHM HTTP」这条务实路线。不填 `temp.lhm_url` → CPU TEMP
那格永远 N/A，但 GPU/CPU占用/内存全部走原生 (`nvml.dll` 直接 syscall +
`gopsutil`)，零外部进程。

本文只讲新版怎么部署 / 运行。设计思路见 [refactor.plan.md](../refactor.plan.md)。

---

## 目录

- [前置](#前置)
- [一、用脚本一键部署（推荐）](#一用脚本一键部署推荐)
- [二、手动运行（调试 / 不想常驻）](#二手动运行调试--不想常驻)
- [三、配置文件](#三配置文件)
- [四、子命令速查](#四子命令速查)
- [五、自启原理](#五自启原理)
- [六、故障排查](#六故障排查)

---

## 前置

1. **Kindle 端已配好 SSH pubkey 免密** ——
   公钥写进 Kindle 的 `/mnt/us/usbnet/etc/authorized_keys`（NiLuJe usbnet 标准位置）。
   旧的 `install.sh` 已经做过这件事的话直接复用。

2. **Windows 端 / macOS 端有对应的私钥**——
   - Windows：`%USERPROFILE%\.ssh\id_ed25519`（或 `id_rsa`，自动发现）
   - macOS：`~/.ssh/id_ed25519`
   - 从 WSL 迁过来：把 `\\wsl$\Ubuntu\home\<user>\.ssh\id_ed25519` 拷到
     `%USERPROFILE%\.ssh\`，然后 `icacls` 收紧权限为 owner-only

3. **Go 工具链**（部署脚本需要 build 一次；预编译二进制部署则不需要）——
   - Windows：`scoop install go` 或 `winget install GoLang.Go`
   - macOS：`brew install go`

---

## 一、用脚本一键部署（推荐）

部署脚本在 [`scripts/`](../scripts/) 下，会完成：
build 二进制 → 拷到标准目录 → 注册登录自启 → 打印 status。

### Windows

**双击运行**（最简单）：

```
scripts\install-windows.cmd
```

`.cmd` 包了一层 PowerShell + `-ExecutionPolicy Bypass`，规避默认策略阻断。

或在终端里：

```powershell
powershell -ExecutionPolicy Bypass -File scripts\install-windows.ps1
```

装到的位置：

- 二进制：`%LOCALAPPDATA%\Programs\kindle-dash\kindle-dash.exe`
- 自启：`HKCU\Software\Microsoft\Windows\CurrentVersion\Run\KindleDash`
- 配置（脚本不动）：`%APPDATA%\kindle-dash\config.json`

卸载：双击 `scripts\uninstall-windows.cmd`，或者
`powershell -ExecutionPolicy Bypass -File scripts\uninstall-windows.ps1`。

### macOS

```bash
chmod +x scripts/install-macos.sh
./scripts/install-macos.sh
```

装到的位置：

- 二进制：`~/.local/bin/kindle-dash`
- 自启：`~/Library/LaunchAgents/com.kindledash.dash.plist`
- 配置：`~/.config/kindle-dash/config.json`
- 自启日志：`~/.cache/kindle-dash/stdout.log` / `stderr.log`

卸载：`./scripts/uninstall-macos.sh`。

### 部署完之后

```powershell
# 配置 Kindle IP（首次安装务必做）
notepad $env:APPDATA\kindle-dash\config.json
# 内容至少要有: { "kindle": { "host": "10.0.0.43" } }

# 自检（验 SSH + Kindle 可达 + eips 存在）
& "$env:LOCALAPPDATA\Programs\kindle-dash\kindle-dash.exe" doctor

# 立即启动（不想等下次登录）
& "$env:LOCALAPPDATA\Programs\kindle-dash\kindle-dash.exe" start

# 看运行状态
& "$env:LOCALAPPDATA\Programs\kindle-dash\kindle-dash.exe" status
```

---

## 二、手动运行（调试 / 不想常驻）

不走部署脚本时，所有命令都对着 `go/kindle-dash.exe`（或 macOS 上的 `go/kindle-dash`）。

### 0. 先 build

```powershell
cd D:\github\KindleSystemMonitor\go
go build -o kindle-dash.exe ./cmd/kindle-dash
```

### 1. 自检 (`doctor`)——出问题第一步永远是它

```powershell
.\kindle-dash.exe doctor
```

会逐项报：config 文件路径、自动发现的 SSH key、ssh-agent 状态、known_hosts、
TCP 连得通否、SSH 握手 + 鉴权、远端 eips 是否存在。任一项失败下面都附带补救命令。

### 2. 单次采集 (`once`)——验证 Windows 原生看得到整机 CPU

```powershell
.\kindle-dash.exe once
# CPU/Mem/GPU/VRAM/温度 一行打出来

.\kindle-dash.exe once --json
# 同样的数据出 JSON 给脚本消费

.\kindle-dash.exe once --save out.png
# 同时渲染一帧 1072x1448 灰度 PNG 到 out.png（不推 Kindle）
```

### 3. 消息屏 (`message`)——欢迎/告别/任意文字

```powershell
.\kindle-dash.exe message --save msg.png "SYSTEM ONLINE`n`n欢迎回来`n高达驾驶员 Liuyi"
# 居中折行、CJK 用内嵌字体不豆腐
```

### 4. 前台主循环 (`run`)——这是常驻模式，Ctrl-C 优雅退出推告别屏

```powershell
.\kindle-dash.exe run
# 用配置文件的 interval / waveform / flush_every

.\kindle-dash.exe run --interval 5 --flush-every 10 --waveform du
# 命令行覆盖

.\kindle-dash.exe run --no-farewell
# 退出时不推告别屏（关机脚本用得着）
```

`run` 的输出长这样：

```
kindle-dash: connecting to root@10.0.0.43:22...
kindle-dash: connected.
kindle-dash: loop (interval=5.0s, waveform="du", flush_every=10)
round 0  cpu=28% mem=49% gpu=68% vram=43%  push=gc16 full              1.392s
round 1  cpu=24% mem=49% gpu=57% vram=43%  push=2 cols + 2 numbers [du] 972ms
...
^C
kindle-dash: exited cleanly.
```

第 0 轮和每 `flush_every` 轮做 `gc16` 整刷洗白，其余只局部 `du`
（变化的 sweep 列 + 变化的数字带）。

---

## 三、配置文件

**位置**

| OS | 路径 |
|----|------|
| Windows | `%APPDATA%\kindle-dash\config.json` |
| macOS | `~/.config/kindle-dash/config.json` |

**最小配置**（只填非默认的字段，其它走代码 `Defaults()`）：

```json
{
  "kindle": { "host": "10.0.0.43" }
}
```

**完整字段**：

```json
{
  "kindle": {
    "host": "10.0.0.43",
    "port": 22,
    "user": "root",
    "identity": "",
    "eips": "/usr/sbin/eips",
    "remote_png": "/tmp/dash.png"
  },
  "loop": {
    "interval_sec": 10,
    "waveform": "du",
    "flush_every": 10,
    "welcome_secs": 10,
    "no_farewell": false
  },
  "messages": {
    "welcome":  ["SYSTEM ONLINE", "", "欢迎回来", "高达驾驶员 Liuyi", "全系统已就绪"],
    "farewell": ["SYSTEM SHUTDOWN", "", "高达驾驶员 Liuyi", "本日作战结束", "后会有期"]
  },
  "temp": {
    "lhm_url": "http://localhost:8085/data.json",
    "cache_ttl": 5
  }
}
```

- `kindle.identity` 留空 → 自动发现 `~/.ssh/id_ed25519/id_ecdsa/id_rsa`，并尝试 ssh-agent
- `loop.waveform` 常见值：`du`（快、有残影）/ `gl16`（16 灰阶慢）/ `a2`（黑白二值快）
- `loop.flush_every = 0` 表示永不周期性 gc16 整刷（仅第 0 轮整刷一次）
- `loop.no_farewell = true` 关闭退出告别屏
- `temp.lhm_url` 留空（默认）→ CPU 温度永远 N/A，**完全无外部依赖**。填了就要求你装并运行
  LibreHardwareMonitor，且在 Options 里打开 Remote Web Server (默认 8085)。`cache_ttl` 控制
  从 LHM 拉 JSON 的 TTL 秒数（默认 5，避免每轮都拉 100KB+）

---

## 四、子命令速查

```
kindle-dash <command> [options]

run [--interval N] [--waveform du] [--flush-every N] [--no-farewell]
    前台主循环，推送到 Kindle。Ctrl-C 优雅退出 + 告别屏。

install [--bin path]
    注册登录自启（Windows: HKCU\Run；macOS: LaunchAgent）。
    默认用 `os.Executable()` 当 binary 路径，部署脚本会显式传 --bin 装好的位置。

uninstall          反向：抹掉自启项、停掉跑着的实例
start              手动启动一个 detached 实例（也就是模拟 install 后下次登录的行为）
stop               杀掉所有 kindle-dash.exe（不含自己）
restart            stop + start
status             看 installed / running 状态

once [--json] [--save out.png]
    单次采集；--save 同时输出渲染好的 dashboard PNG

message --save out.png "text"
    渲染一屏居中文字 PNG（不推 Kindle，仅生成图片）

doctor [--config x.json --host ... --port ... --user ... --identity ...]
    SSH/Kindle 可达性自检

version / help     版本号 / 帮助
```

---

## 五、自启原理

### Windows

`HKCU\Software\Microsoft\Windows\CurrentVersion\Run\KindleDash =
"C:\...\kindle-dash.exe" run`

Windows 在用户登录时读 HKCU\Run 直接 CreateProcess 这一项。**纯用户级、不弹 UAC、
不需要管理员**。代价是登录时会有一瞬控制台窗口闪一下（Go 默认是 console
subsystem）。如果想完全无窗口，自己用 `-ldflags="-H=windowsgui"` 重新 build
一个无窗口版本：

```powershell
go build -ldflags="-H=windowsgui" -o kindle-dashw.exe ./cmd/kindle-dash
```

然后手动把 HKCU\Run 那项改指向 `kindle-dashw.exe`。注意：无窗口版本你在终端跑
`run / once / doctor` 也看不到输出，所以建议保留两个：
日常用 `kindle-dash.exe`，自启项指向 `kindle-dashw.exe`。

### macOS

`~/Library/LaunchAgents/com.kindledash.dash.plist`，launchctl 在登录时加载。
`KeepAlive=true` 让进程崩了 launchd 会自动拉起。

---

## 六、故障排查

**doctor 全绿但 Kindle 没画面**——
看 `kindle-dash run` 的输出，确认 round 0 完成且没报错。Kindle 屏幕 ~1s 才会响应。

**`run` 启动后立刻退出，提示 "another kindle-dash is running"**——
pidfile 锁还在。看 `%LOCALAPPDATA%\kindle-dash\dash.pid`（macOS：`~/.cache/...`），
里面那个 PID 已经死了的话直接删 pidfile 再跑。

**SSH 鉴权失败 (`no supported methods remain`)**——
跑 `doctor` 看「ssh keys (auto-discovery)」那段，确认列出的私钥的公钥确实在
Kindle 的 `/mnt/us/usbnet/etc/authorized_keys` 里。可能是私钥拷过来了但公钥没推。

**host key mismatch**——
重装/换了 Kindle 会触发。按 doctor 的提示从 `~/.ssh/known_hosts` 删掉对应行
再跑。**别盲目接受新 key**——确认是你自己换的硬件再删。

**Windows 登录后没自启**——
确认 `reg query HKCU\Software\Microsoft\Windows\CurrentVersion\Run /v KindleDash`
有值，且指向的 exe 真存在。HKCU\Run 不会有 retry / 重试机制——首次失败就静默。

**macOS 自启后没画面，看不到 stdout**——
`tail -F ~/.cache/kindle-dash/stderr.log`。

**想看 dashboard 长啥样但不想推 Kindle**——
`kindle-dash once --save /tmp/dash.png && open /tmp/dash.png`（macOS）
或 `kindle-dash once --save dash.png; Start-Process dash.png`（Windows）。
