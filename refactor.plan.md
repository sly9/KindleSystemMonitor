# 重构计划：去 WSL / 去 LHM，跨平台 (Windows + macOS) Go 重写

> 本文件是「另起一套 Go 实现」的设计文档。**现有 Python 代码 (`kindle_dash.py` /
> `kindlectl` / `install.sh` / `config.sh`) 一律不动**，Go 版在独立目录里平行开发，
> 跑通验证后再考虑是否替换。先有这份文档，再写代码。

---

## 0. 为什么重写、为什么 Go

### 根因诊断
现有架构有两个依赖：**WSL** 和 **LibreHardwareMonitor (LHM)**。但它们不是两个独立问题，是**同一个根因**：

> 服务端跑在 WSL 里 → WSL 里的 psutil 只能看到 WSL 子系统、看不到 Windows 整机
> → 所以 CPU 占用 / 温度 / 内存被迫去 Windows 端的 LHM 读。

**只要服务端从 WSL 挪到宿主机原生运行，psutil 级别的接口就能直接看到整机，LHM 的主要工作
(cpu_load、ram) 当场消失。** LHM 唯一还剩的事是 CPU 温度，单独降级处理即可。

所以本质动作是：**别在 WSL 里跑，回到宿主机原生跑；并把所有 OS 相关的脏活抽象到平台层。**

### 为什么选 Go（而不是继续 Python / Node / Rust）
这是一个**长驻后台服务 + 网络推送**，Go 正是为这类「基础设施二进制」而生。它一次性删掉
六样附带复杂度：

| 现有痛点 | Go 怎么解决 |
|---------|------------|
| Windows 上没有原生 Python，部署要先装运行时 | **单个静态二进制**，零运行时依赖；一台机器交叉编译出 Win/Mac 两个文件 |
| Windows 原生 OpenSSH 不支持 ControlMaster | **进程内持久 SSH 连接** (`golang.org/x/crypto/ssh`)，自己持有一条长连接，复用 session |
| `fcntl` 单实例锁是 Unix-only | 跨平台 pidfile + 存活检测 |
| `kindlectl` 是 bash + VBS + 启动文件夹 + `wsl.exe` | 二进制自带 `install` 子命令，自己写 launchd plist / 任务计划 |
| 字体靠扫 `/mnt/c/...` 挂载路径，脆 | **`go:embed` 把 CJK 字体打进二进制**，零发现、CJK 永不豆腐块 |
| LHM 整套 web server + powershell + 网关 IP | 删除；指标走 gopsutil / go-nvml 原生读 |

代价：约 700 行 Python 要用 Go 重写，渲染从 Pillow 换成 `fogleman/gg`。渲染层布局几何是
纯计算，可 1:1 照搬常量。

### 不变的部分
- **Kindle 端完全不动**：依旧是「网络相框」，接收 PNG → `/usr/sbin/eips` 刷屏。
- **协议不动**：管道法 `cat > /tmp/dash.png && /usr/sbin/eips -g ... -w <波形>`。
- **刷新策略不动**：sweep 局部快刷 + 周期 gc16 整屏洗白 + 数字带按需局部刷。
- **屏幕几何不动**：1072×1448，上 CPU / 下 GPU，三列大数字 + 镜像 sweep 历史图。

---

## 1. 目标平台与边界

- **只支持 Windows 原生 与 macOS**。不考虑 Linux 桌面。
- 用 Go build tag (`//go:build windows` / `//go:build darwin`) 分流 OS 相关实现。
- 一份代码，交叉编译两个产物：`kindle-dash.exe` (Windows) / `kindle-dash` (macOS)。

---

## 2. 目录结构（新目录，与现有 Python 并存）

建议放在仓库下新目录 `go/`（最终名字可改），与现有文件互不干扰：

```
go/
  go.mod
  cmd/kindle-dash/
    main.go                 # CLI 入口：子命令 + flag 解析
  internal/
    config/
      config.go             # 读取 JSON 配置 + flag 覆盖
    metrics/
      metrics.go            # Metrics 结构体 + Provider 接口 + 组装逻辑
      provider_common.go    # gopsutil 读 cpu/mem（两 OS 共用）
      gpu_nvidia.go         # go-nvml 读 NVIDIA GPU（//go:build windows）
      gpu_darwin.go         # macOS GPU（powermetrics，可选；默认 N/A）
      temp_windows.go       # Windows 温度（可选；默认 N/A，见 §6）
      temp_darwin.go        # macOS 温度（powermetrics，可选；默认 N/A）
    render/
      dashboard.go          # 有状态 framebuffer + sweep + 数字带（移植 Dashboard 类）
      message.go            # 欢迎/告别/自定义整屏文字
      fonts.go              # go:embed 字体加载
      assets/font.ttf       # 内嵌的开源 CJK 字体（见 §7）
    transport/
      transport.go          # Transport 接口
      ssh.go                # 进程内持久 SSH 客户端（替代 ControlMaster）
    service/
      service.go            # install/uninstall/start/stop/status 公共逻辑
      service_darwin.go     # launchd LaunchAgent plist
      service_windows.go    # 任务计划 (schtasks) 登录自启，无窗口
    lock/
      lock.go               # 跨平台单实例锁（pidfile + 存活检测）
  config.example.json
  README.md
```

---

## 3. 数据采集层（这一层就废掉 LHM）

### 3.1 统一数据结构
对齐现有 `collect_metrics` 返回的 dict。用指针表达「读不到 = N/A」：

```go
type Metrics struct {
    CPU     *float64 // 整机 CPU 占用 %
    Mem     *float64 // 物理内存 %
    GPU     *float64 // GPU 利用率 %
    VRAM    *float64 // 显存 %
    CPUTemp *float64 // CPU 温度 °C（可选，可能 nil）
    GPUTemp *float64 // GPU 温度 °C
}
```

### 3.2 Provider 接口
```go
type Provider interface {
    Read() Metrics  // 永不 panic；读不到的项留 nil
}
```
组装顺序照搬现有逻辑：CPU/Mem 走原生（gopsutil），GPU 走 go-nvml / powermetrics，
温度按平台可选源；任一项失败只置 nil，不影响其他项。

### 3.3 各平台实现
| 指标 | Windows | macOS |
|------|---------|-------|
| CPU 占用 % | `gopsutil/cpu.Percent`（**整机，准**） | 同左 |
| 内存 % | `gopsutil/mem.VirtualMemory` | 同左 |
| GPU 利用率 / 显存 | `go-nvml`（RTX 5090 直接可用）；降级 `nvidia-smi` | Apple 芯片无 nvidia：`powermetrics`（需 sudo）或先 N/A |
| CPU 温度 | 见 §6（默认 N/A） | `powermetrics`（需 sudo）或先 N/A |
| GPU 温度 | `go-nvml` 透传 | 同 GPU |

依赖（go.mod）：
- `github.com/shirou/gopsutil/v4` — cpu / mem / host
- `github.com/NVIDIA/go-nvml` — NVIDIA（仅 windows build tag 编入）
- 标准库 `os/exec` — 降级 shell `nvidia-smi` / `powermetrics`

---

## 4. 渲染层（移植，不重新设计）

- 库：`github.com/fogleman/gg`（freetype 文字 + 矢量绘制）。
- **几何常量 1:1 照搬**现有 `kindle_dash.py`：`WIDTH/HEIGHT=1072/1448`、`F_VAL/F_LAB`、
  `COL_W`、`COL_CENTERS`、`NUM_BAND`、`CHART_TOP/BOT`、`BLOCKS` 等，保证画面一致。
- **`Dashboard` 有状态对象移植**：维持一份 framebuffer，`compose()` 每 tick 只画变化的
  sweep 列 + 变化的数字带，返回需局部刷新的区域列表。sweep 画满清空从左重来的逻辑照搬。
- **灰度输出**：eips 要 8-bit 灰度 PNG。gg 内部是 RGBA，编码前转成 `image.Gray` 再
  `png.Encode`。（这是与 Pillow `L` 模式对齐的关键一步，单独写个 helper。）
- `message.go`：欢迎 / 告别 / 自定义消息整屏，按宽度折行居中（移植 `_draw_wrapped_centered`）。

---

## 5. 传输层（进程内 SSH，删掉整个 ControlMaster 问题）

```go
type Transport interface {
    PushRegion(img image.Image, x, y int, waveform string) error // 局部刷
    FullRefresh(img image.Image, waveform string, clear bool) error // 整屏刷
    Close() error
}
```

- 实现：`golang.org/x/crypto/ssh`，进程内持有一个持久 `*ssh.Client`；每次推送开一个
  session，把 PNG 字节写进 remote stdin，执行
  `cat > /tmp/dash.png && /usr/sbin/eips -g /tmp/dash.png -x <x> -y <y> -w <wf> [-f]`。
- 连接懒建立 + 断线自动重连。**一条长连接复用 session = ControlMaster 的效果，但全在进程内**，
  Windows / macOS 行为一致，不依赖系统 ssh 命令。
- 认证：读 `~/.ssh/id_*` 私钥（与现有免密一致）。Kindle 端不用任何改动。
- 配置项：host/ip、port(22)、user(root)、私钥路径、远端 PNG 路径、eips 全路径。

---

## 6. 温度 / macOS GPU —— 语言救不了的硬事实

**这是 OS 层限制，不是换语言能绕过的，务实做成可选、读不到显示 N/A：**

- **Windows CPU 温度**：原生无简单 API（要 WMI/管理员，或装 LHM/OHM）。默认 **N/A**；
  保留一个可选配置 `lhm_url`，填了就去读 LHM 仅取温度（向后兼容老用户）。
- **macOS（Apple 芯片）GPU 占用 / 温度**：需 `sudo powermetrics`。默认 **N/A**；
  提供可选开关，开了才 shell powermetrics（需用户配 sudo 免密或以高权限跑服务）。
- 设计上温度/Mac-GPU 永远是「锦上添花」，缺失不影响主链路与 CPU/RAM/NVIDIA-GPU 显示。

---

## 7. 字体（用内嵌彻底消灭「找字体」这件事）

- 现有方案扫 `/mnt/c/Users/*/...` 找 PingFang，脆且依赖 WSL 挂载。
- 新方案：**`go:embed` 把一份开源 CJK 字体打进二进制**（如 Source Han Sans / Noto Sans
  CJK 的子集，或思源黑体 Bold）。零发现、零依赖系统字体、CJK 永不豆腐块、两 OS 完全一致。
- 仅嵌一个字重即可（大数字和标签共用，按 size 区分）。注意选**许可证允许嵌入分发**的字体
  （SIL OFL 的思源/Noto 系列 OK）。

---

## 8. 配置（去 bash，改成可读配置 + flag）

- 现有 `config.sh` 是 bash source，跨平台不可用。
- 新方案：**JSON 配置文件**（用 Go 标准库 `encoding/json`，不引第三方 TOML，省依赖）。
  例 `config.example.json`：
  ```json
  {
    "kindle": { "host": "10.0.0.43", "port": 22, "user": "root",
                "identity": "~/.ssh/id_ed25519", "eips": "/usr/sbin/eips" },
    "loop":   { "interval_sec": 10, "waveform": "du", "flush_every": 10 },
    "temp":   { "lhm_url": "", "mac_powermetrics": false }
  }
  ```
- 命令行 flag 可覆盖任意配置项（`--interval` / `--waveform` / `--host` 等，对齐现有）。
- 配置文件位置：`~/.config/kindle-dash/config.json`（mac）/ `%APPDATA%\kindle-dash\config.json`（win），
  或 `--config` 指定。

---

## 9. 服务管理（替代 kindlectl，二进制自带）

CLI 子命令直接对齐现有 `kindlectl`，但跨平台、无外部脚本：

| 子命令 | 行为 |
|--------|------|
| `kindle-dash run` | 前台常驻主循环（= 现有默认） |
| `kindle-dash once [--save x.png]` | 跑一次（调试） |
| `kindle-dash message "文字"` | 推一屏文字 |
| `kindle-dash install` | 装登录自启 + 立即启动（= `kindlectl enable`） |
| `kindle-dash uninstall` | 停止 + 卸载自启 |
| `kindle-dash start/stop/restart/status` | 进程管理（pidfile 单实例） |

自启实现（**用户级、免管理员**，适合自动登录的墙面机）：
- **macOS**：写 `~/Library/LaunchAgents/com.kindledash.dash.plist`，`launchctl load`。
- **Windows**：`schtasks` 创建「登录时触发」任务，跑 `kindle-dash.exe run`，无窗口
  （或放启动文件夹快捷方式）。不再需要 VBS / wsl.exe。
- 备选：`github.com/kardianos/service` 统一封装三平台服务安装——但它装的是系统级服务、
  多半要管理员权限。墙面机自动登录场景下用户级自启更合适，**默认手写用户级自启**，
  kardianos 作为备选记录在案。

---

## 10. 单实例锁（替代 fcntl）

- 跨平台 pidfile：写 pid 到 `~/.cache/kindle-dash/dash.pid`（mac）/ `%LOCALAPPDATA%`（win），
  启动时检测该 pid 是否存活（`gopsutil/process` 或 `os.FindProcess`+signal 0）。
- 可选加 OS 级锁（windows 命名 mutex / unix flock via `golang.org/x/sys`）做加固，先 pidfile 够用。

---

## 11. 分阶段实施（每阶段可独立验证）

> 全程不碰现有 Python 代码。每个阶段产出可运行/可看的东西。

- **阶段 1 — 骨架 + 采集（验证「LHM 已死」）**
  `go.mod`、config、metrics 接口 + 原生 provider（gopsutil cpu/mem + go-nvml GPU）。
  `kindle-dash once` 打印整机指标。**验收：在 Windows 原生跑，CPU/RAM 是整机真实值（不再 ~0）。**

- **阶段 2 — 渲染（验证画面）**
  移植 `Dashboard` + 内嵌字体 + 灰度 PNG 输出。`kindle-dash once --save out.png`。
  **验收：本地 PNG 与现有 Python 版画面一致（布局/字号/sweep）。**

- **阶段 3 — 传输 + 主循环（验证上屏）**
  进程内 SSH，推到真 Kindle，跑完整 sweep 循环 + 周期 gc16。
  **验收：Kindle 上画面与现有版行为一致；Windows 原生下无 ControlMaster 也能稳定推。**

- **阶段 4 — 服务化**
  `install/uninstall/start/stop/status` + 欢迎/告别屏。
  **验收：登录自启、优雅停止推告别屏，两 OS 各自跑通。**

- **阶段 5 — 可选增强**
  温度 provider（Windows 可选 LHM-only / mac powermetrics）、macOS GPU。
  **验收：开了能读到温度，关了/读不到优雅显示 N/A。**

---

## 12. 待决 / 风险点

1. **macOS 验证机**：阶段 3 起需要一台 Mac 实测（SSH/字体/launchd）。Windows 侧你现有机器可测。
2. **gg 渲染像素级对齐**：freetype 与 Pillow 的字形度量/anchor 略有差异，阶段 2 需对图微调
   （anchor="ma" 顶部居中等价实现）。
3. **go-nvml 在 5090 + 最新驱动**：若绑定版本滞后，降级 `nvidia-smi` 解析（已规划）。
4. **内嵌字体选型与许可**：选 SIL OFL 的思源/Noto，确认可嵌入分发。
5. **是否最终替换 Python 版**：本计划只做「平行另一套」，跑通后是否退役 Python 版另行决定。
