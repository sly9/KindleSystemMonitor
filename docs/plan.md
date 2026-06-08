# Kindle PW3 墙面 Dashboard —— 项目说明 & 实施计划

> 这份文档用于在 WSL 里配合 Claude Code 开发。它记录了**我们要做什么、整体架构、已确认的环境事实、当前阶段的具体任务**,以及后续迭代方向。Claude Code 可以直接基于此文档开始写代码。

---

## 1. 项目目标

把一台越狱后的 **Kindle Paperwhite 3 (PW3, codename `muscat`)** 改造成墙面智能终端,显示自定义 dashboard。

- **远期目标**:墙面常驻,显示 Home Assistant 面板 / 自定义信息(传感器、日历、猫砂盆状态等),并支持触摸交互(按钮控制 HA 实体)。
- **当前阶段(本文档聚焦)**:做一个**玩具级工具**,在 WSL 里采集本机 CPU / 内存 / GPU 使用率,渲染成数字,推送到 Kindle 屏幕显示。目的是**跑通整条链路**,为后续局部快刷 / 交互打基础。

---

## 2. 整体架构

核心思想:**Kindle 不做任何渲染**,只当一块"网络相框"。所有计算、绘图都在服务端(WSL)完成,Kindle 端只负责接收 PNG 并用 `eips` 刷到屏幕上。

```
┌─────────────────────────────┐      SSH (连接复用)      ┌──────────────────────┐
│   服务端 = WSL                │  ───  推送 PNG  ───→     │   Kindle PW3          │
│                              │                          │  (10.0.0.43)          │
│  - 采集 CPU/MEM/GPU           │                          │                       │
│  - Pillow 画成灰度 PNG         │                          │  - 接收 /tmp/dash.png  │
│  - 1072×1448, 8-bit Gray     │                          │  - /usr/sbin/eips 渲染 │
│  - 循环每 N 秒                 │                          │                       │
└─────────────────────────────┘                          └──────────────────────┘
```

---

## 3. 已确认的环境事实(重要,写代码时按这些来)

| 项目 | 值 / 说明 |
|------|-----------|
| Kindle 型号 | Paperwhite 3 (`muscat`) |
| Kindle IP | `10.0.0.43` |
| Kindle 屏幕分辨率 | **1072 × 1448**,300 ppi,灰度(竖向) |
| Kindle SSH 用户 | `root` |
| Kindle SSH 已配置 | 是,USB + WiFi SSH 均已跑通(NiLuJe usbnet 0.22.N,搜索框 `;un` 触发) |
| **`eips` 路径** | **不在 PATH 中,必须用全路径 `/usr/sbin/eips`** |
| 服务端 | WSL (Windows 上的 Linux 子系统) |
| GPU | RTX 5090(WSL 通过驱动透传可读) |

> ⚠️ **WSL 资源读数的注意点**:在 WSL 里用 psutil 读到的 CPU/内存是 **WSL 子系统**的视角,不一定等于整台 Windows 机器的全局占用。GPU 因为是硬件透传,读到的是真实显卡。玩具阶段用 WSL 读数即可;若以后要"整机精确占用",需改为 Windows 侧采集。

---

## 4. 各环节技术方案

### 4.1 数据采集(WSL 端)

- **CPU**:`psutil.cpu_percent(interval=1)`
- **内存**:`psutil.virtual_memory().percent`
- **GPU(RTX 5090)**:两种方式,优先前者
  - `pynvml`(NVIDIA 官方 Python 绑定):`nvmlDeviceGetUtilizationRates()` 拿利用率,`nvmlDeviceGetMemoryInfo()` 拿显存。输出干净。
  - 降级方案:解析 `nvidia-smi --query-gpu=utilization.gpu,memory.used,memory.total --format=csv,noheader,nounits`
  - 代码里建议做容错:GPU 读不到时显示 `N/A`,不要让整个脚本崩。

依赖安装:
```bash
pip install psutil pynvml pillow
```

### 4.2 出图(WSL 端)

- 用 **Pillow (PIL)** 在内存里画 1072×1448 的 **`L` 模式(8-bit 灰度)** 图。
- 白底,用大号 TTF 字体写三行数字,例如:
  ```
  CPU   37%
  MEM   54%
  GPU   12%
  ```
- 字体:WSL 里 `/usr/share/fonts/` 下找,或 `apt install fonts-dejavu`。数字要够大,eink 上才清晰。
- 存成 PNG(`-depth 8` 灰度)。
- (可选)加字符版进度条,如 `████░░░░░░`,几乎零成本。

### 4.3 推送 + 刷新(到 Kindle)

**关键:`eips` 必须用全路径 `/usr/sbin/eips`。**

单次刷新命令(管道法,一次 SSH 连接同时完成传输 + 刷新):
```bash
cat dash.png | ssh kindle "cat > /tmp/dash.png && /usr/sbin/eips -g /tmp/dash.png"
```

清屏消残影可用 `/usr/sbin/eips -c`,强制全刷用 `/usr/sbin/eips -f -g ...`。

### 4.4 SSH 连接复用(避免每次握手开销)

Kindle 的 dropbear 在弱 CPU 上握手慢,**用 OpenSSH 的 ControlMaster 复用连接**。Kindle 端不用改任何东西。

在 WSL 的 `~/.ssh/config` 加:
```
Host kindle
    HostName 10.0.0.43
    User root
    ControlMaster auto
    ControlPath ~/.ssh/cm-%r@%h:%p
    ControlPersist 600
```

之后命令统一用 `kindle` 别名,第一次握手后复用,几乎零开销。

> 若出现第二个命令卡住,可能是 dropbear `MaxSessions` 限制(进阶,大概率用不到)。

### 4.5 循环

- 外层 `while True` + `time.sleep(N)`,每 N 秒采集 → 画图 → 推送。
- 玩具阶段 N 设大点(10–30 秒),避免 eink 频繁全刷(费屏 + 闪)。
- 建议加 `--once` 参数:只跑一次,方便调试。
- 建议每轮打印耗时(采集 / 画图 / 推送分别多久),直观看到瓶颈。

---

## 5. 当前阶段任务清单(给 Claude Code)

做一个**单文件 Python 脚本**,跑在 WSL 里:

1. [ ] 采集 CPU(psutil)、内存(psutil)、GPU(pynvml,降级 nvidia-smi)
2. [ ] 用 Pillow 画成 1072×1448 的 8-bit 灰度 PNG,三个大数字
3. [ ] 用 **ControlMaster 复用连接**,管道法推图到 `10.0.0.43` 并用 `/usr/sbin/eips -g` 刷新
4. [ ] 每 N 秒循环;支持 `--once` 只跑一次
5. [ ] 每轮打印各阶段耗时
6. [ ] GPU / 字体等做容错,读不到不崩

附带交付:
- `~/.ssh/config` 里 `kindle` 别名那段配置
- 依赖安装命令
- 字体处理(找不到默认字体时的降级)

---

## 6. 已知的 eink 刷新特性(背景知识,本阶段先不优化)

- **物理刷新速度**(单次,取决于 waveform):
  - A2(快速单色):约 120–260ms,有残影累积
  - DU(快速灰度):约 260–450ms
  - GC16(完整 16 级灰度全刷):约 600ms–1s+,会黑白翻转闪一下消残影
- **只刷一小块**最快约 1/4 秒(A2 + 本地生成 + FBInk 常驻);每次都联网拉数据则 0.5–2 秒,网络主导。
- A2 局部快刷会**累积残影**,需"每刷 N 次 → GC16 全刷洗白"的策略。
- eink **不适合高频更新**(每秒多次),残影 + 寿命都不划算。

---

## 7. 后续迭代方向(本阶段之后)

1. **局部快刷**:从"整图重画 + 整屏 eips" 进化为"只重画数字那一小块 + FBInk 局部 A2 快刷",把延迟压到物理极限(~200–300ms)。FBInk 在 usbnet 包里已 bundle。
2. **接入 Home Assistant**:
   - 服务端用 `sibbl/hass-lovelace-kindle-screensaver` 类容器,把 HA Lovelace 视图截图成 1072×1448 灰度 PNG。
   - 或自己写 HTML 页面 + Puppeteer 渲染(kindle-dash 类)。
3. **触摸交互(按钮)**:
   - Kindle 底层是 Linux,触摸是 `/dev/input/eventX` 的 evdev 设备。
   - 常驻脚本读触摸坐标 → 判断落在哪个按钮矩形 → `curl` 调 HA REST API → 局部刷新反馈。
   - 读触摸可用 FBInk 工具集里的 `waitforevent`。
   - 注意:按钮按下要立刻给视觉反馈(局部刷一下),否则 eink 延迟会让人以为没点上而连点。
4. **常驻 / 开机自启 / 防休眠 / WiFi 保活**:
   - 防休眠:`lipc-set-prop -i com.lab126.powerd preventScreenSaver 1`
   - WiFi 保活:`iwconfig wlan0 power off`
   - 开机自启:usbnet 文件夹放空 `auto` 文件(README 方式),或 KUAL 启动项。
   - 隐藏 UI:停 framework(cvm)让屏幕只剩 dashboard。
   - **这些重启都会复位,最终要落到开机自启脚本里。**
5. **Kindle 端常驻服务(极限延迟)**:若 ControlMaster 还不够快,可在 Kindle 端跑轻量服务监听端口,WSL 直接推图,免 SSH 开销。属于过度工程,真有需求再上。

---

## 8. 快速参考:已跑通的测试命令(链路验证用)

```bash
# 下载测试图 → 转 1072×1448 灰度 → 推 Kindle → eips 渲染
wget -O /tmp/test_src.png https://picsum.photos/1072/1448
convert /tmp/test_src.png -colorspace Gray -resize 1072x1448 \
  -gravity center -background white -extent 1072x1448 -depth 8 /tmp/dash.png
cat /tmp/dash.png | ssh kindle "cat > /tmp/dash.png && /usr/sbin/eips -c; /usr/sbin/eips -g /tmp/dash.png"
```
