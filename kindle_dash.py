#!/usr/bin/env python3
"""Kindle PW3 墙面 Dashboard —— 玩具阶段单文件脚本.

在 WSL 端采集 CPU/MEM/GPU,用 Pillow 画成 1072x1448 的 8-bit 灰度 PNG,
再用 SSH (ControlMaster 复用连接) 管道法推到 Kindle 并用 /usr/sbin/eips 刷新.

用法:
    python3 kindle_dash.py --once                 # 只跑一次 (调试)
    python3 kindle_dash.py --interval 15          # 每 15 秒循环
    python3 kindle_dash.py --once --clear         # 先 eips -c 洗白再刷
    python3 kindle_dash.py --once --message "你好" # 推一条自定义消息
    python3 kindle_dash.py --once --save out.png   # 顺便存一份本地 PNG 看效果

前置:
    ~/.ssh/config 里有 `kindle` 别名 (见 plan §4.4)
    pip install --break-system-packages psutil pynvml pillow
"""

from __future__ import annotations

import argparse
import fcntl
import io
import os
import signal
import subprocess
import sys
import time

from PIL import Image, ImageDraw, ImageFont

# ---- Kindle PW3 屏幕参数 (竖向, 见 plan §3) ----
WIDTH, HEIGHT = 1072, 1448
EIPS = "/usr/sbin/eips"
REMOTE_PNG = "/tmp/dash.png"
PIDFILE = "/tmp/kindle_dash.pid"  # 单实例锁

# ---- 中二欢迎 / 告别屏 (随便改) ----
WELCOME_LINES = [
    "SYSTEM ONLINE",
    "",
    "欢迎回来",
    "高达驾驶员 Liuyi",
    "全系统已就绪",
]
FAREWELL_LINES = [
    "SYSTEM SHUTDOWN",
    "",
    "高达驾驶员 Liuyi",
    "本日作战结束",
    "后会有期",
]

# 候选字体路径 (按优先级), 找不到再降级到 PIL 内置位图字体.
# 优先含 CJK 字形的字体, 否则中文会变 □ 豆腐块. 自动发现, 不写死 Windows 用户名:
# PingFang SC (Windows 用户字体目录, 中英数都漂亮, eink 上 Semibold 够粗清晰)
# → 微软雅黑 → DroidSansFallback (Linux 自带 CJK 兜底) → DejaVu (仅 Latin).
def _font_candidates() -> list[str]:
    import glob
    cands: list[str] = []
    for name in ("PingFangSC-Semibold.otf", "PingFangSC-Medium.otf",
                 "PingFangSC-Regular.otf"):
        cands += sorted(glob.glob(
            f"/mnt/c/Users/*/AppData/Local/Microsoft/Windows/Fonts/{name}"))
    cands += [
        "/mnt/c/Windows/Fonts/msyhbd.ttc",  # 微软雅黑 Bold (含 CJK)
        "/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf",  # CJK 兜底
        "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf",
    ]
    return cands


FONT_CANDIDATES = _font_candidates()


# ----------------------------------------------------------------------------
# 单实例锁
# ----------------------------------------------------------------------------
_lock_fh = None


def acquire_lock() -> bool:
    """flock PIDFILE 保证只有一个常驻实例. 拿到返回 True, 否则 False."""
    global _lock_fh
    _lock_fh = open(PIDFILE, "a+")
    try:
        fcntl.flock(_lock_fh, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except OSError:
        return False
    _lock_fh.seek(0)
    _lock_fh.truncate()
    _lock_fh.write(str(os.getpid()))
    _lock_fh.flush()
    return True


# ----------------------------------------------------------------------------
# 数据采集
# ----------------------------------------------------------------------------
def read_cpu_mem(sample: float = 1.0) -> tuple[float | None, float | None]:
    """返回 (cpu%, mem%). 读不到返回 None, 不崩.

    sample: psutil 采样窗口秒. >0 阻塞采样该时长拿瞬时占用 (准但慢);
            <=0 非阻塞 (interval=None), 立即返回上次调用以来的增量 (首次返回 0).
            极限测速时设 0, 避免每轮白白阻塞 1s.
    """
    try:
        import psutil

        cpu = psutil.cpu_percent(interval=sample if sample > 0 else None)
        mem = psutil.virtual_memory().percent
        return cpu, mem
    except Exception as e:  # noqa: BLE001
        print(f"[warn] CPU/MEM 读取失败: {e}", file=sys.stderr)
        return None, None


def read_gpu() -> tuple[float | None, float | None, float | None]:
    """返回 (gpu_util%, gpu_mem%, gpu_temp°C). 优先 pynvml, 降级 nvidia-smi.

    GPU 温度走 nvidia-smi/pynvml (硬件透传, 不依赖 LHM), 读不到返回 None.
    """
    # 方案 A: pynvml
    try:
        import warnings

        with warnings.catch_warnings():
            warnings.simplefilter("ignore", FutureWarning)
            import pynvml

        pynvml.nvmlInit()
        try:
            h = pynvml.nvmlDeviceGetHandleByIndex(0)
            util = pynvml.nvmlDeviceGetUtilizationRates(h).gpu
            mem = pynvml.nvmlDeviceGetMemoryInfo(h)
            mem_pct = 100.0 * mem.used / mem.total if mem.total else None
            temp = float(pynvml.nvmlDeviceGetTemperature(h, pynvml.NVML_TEMPERATURE_GPU))
            return float(util), mem_pct, temp
        finally:
            pynvml.nvmlShutdown()
    except Exception:  # noqa: BLE001
        pass

    # 方案 B: 解析 nvidia-smi
    try:
        out = subprocess.run(
            [
                "nvidia-smi",
                "--query-gpu=utilization.gpu,memory.used,memory.total,temperature.gpu",
                "--format=csv,noheader,nounits",
            ],
            capture_output=True,
            text=True,
            timeout=5,
            check=True,
        ).stdout.strip().splitlines()[0]
        util, mem_used, mem_total, temp = (float(x) for x in out.split(","))
        mem_pct = 100.0 * mem_used / mem_total if mem_total else None
        return util, mem_pct, temp
    except Exception as e:  # noqa: BLE001
        print(f"[warn] GPU 读取失败: {e}", file=sys.stderr)
        return None, None, None


# CPU 温度: WSL 读不到宿主机传感器, 从 Windows 端的 LibreHardwareMonitor web server 取.
# 防火墙放行后, 直接 curl WSL 网关 IP (= Windows 宿主机) 最快 (~7ms). 没放行/探测失败时,
# 降级让 powershell.exe 在 Windows 侧访问 localhost 再把 JSON 经 stdout 传回 (~0.4s).
_PS = "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe"


def windows_host_ip() -> str | None:
    """WSL 默认路由网关 = Windows 宿主机 IP (不受 DHCP 影响, 比硬编码局域网 IP 稳)."""
    try:
        out = subprocess.run(
            ["ip", "route", "show", "default"],
            capture_output=True, text=True, timeout=3, check=True,
        ).stdout.split()
        return out[out.index("via") + 1] if "via" in out else None
    except Exception:  # noqa: BLE001
        return None


def default_lhm_url() -> str:
    """默认 LHM 地址: 优先网关 IP 直连; 取不到则 localhost (走 powershell)."""
    ip = windows_host_ip()
    return f"http://{ip}:8085/data.json" if ip else "http://localhost:8085/data.json"
# CPU 温度传感器名优先级 (不同 CPU/厂商命名不同).
_CPU_TEMP_NAMES = ["Core (Tctl/Tdie)", "CPU Package", "Core (Tctl)", "Core Max"]
# 简单缓存: url -> (时间戳, 解析结果 dict), 避免每轮都掏一次 LHM.
_lhm_cache: dict[str, tuple[float, dict]] = {}


def _fetch_lhm_json(url: str) -> dict | None:
    """取 LHM /data.json. localhost 走 powershell (Windows 侧 localhost), 否则 curl 直连."""
    import json
    from urllib.parse import urlparse

    host = urlparse(url).hostname or ""
    try:
        if host in ("localhost", "127.0.0.1"):
            out = subprocess.run(
                [_PS, "-NoProfile", "-Command",
                 f"(Invoke-WebRequest -UseBasicParsing -Uri '{url}' -TimeoutSec 5).Content"],
                capture_output=True, text=True, timeout=15, check=True,
            ).stdout
        else:
            out = subprocess.run(
                ["curl", "-s", "--max-time", "5", url],
                capture_output=True, text=True, timeout=8, check=True,
            ).stdout
        return json.loads(out)
    except Exception:  # noqa: BLE001
        return None


def _parse_lhm(data: dict) -> dict:
    """从 LHM JSON 解析整机真实读数: CPU 占用/温度 + 物理内存%.

    psutil 在 WSL 里只能看到 WSL 子系统, 读不到 Windows 整机占用 (游戏跑在 Windows
    侧时 CPU 会显示 ~0). 这里取 LHM 的整机视角传感器.
    """
    # 树结构: Sensor / 主机 / 硬件 / 分组(Load|Temperatures...) / 传感器
    # 硬件名是传感器的"祖父", 所以按完整路径匹配, 不能只看直接父节点.
    sensors: list[tuple[tuple, str, float]] = []  # (祖先路径, 传感器名, 数值)

    def walk(n, path):
        name, v = n.get("Text", ""), str(n.get("Value", ""))
        if "°C" in v or "%" in v:
            try:
                sensors.append(
                    (path, name, float(v.split("°")[0].replace("%", "").strip())))
            except ValueError:
                pass
        for c in n.get("Children", []):
            walk(c, path + (name,))

    walk(data, ())

    def find(pred):
        for path, n, val in sensors:
            if pred(path, n):
                return val
        return None

    cpu_temp = None
    for tn in _CPU_TEMP_NAMES:
        cpu_temp = find(lambda path, n, tn=tn: n == tn)
        if cpu_temp is not None:
            break
    return {
        "cpu_load": find(lambda path, n: n == "CPU Total"),
        "cpu_temp": cpu_temp,
        # 物理内存% ("Total Memory"); 区别于 "Virtual Memory" 的提交内存
        "ram": find(lambda path, n: "Total Memory" in path and n == "Memory"),
    }


def read_lhm(url: str, cache_ttl: float = 5.0) -> dict:
    """取 LHM 整机读数 (cpu_load/cpu_temp/ram), 带 TTL 缓存. 读不到各项为 None."""
    now = time.monotonic()
    cached = _lhm_cache.get(url)
    if cached and now - cached[0] < cache_ttl:
        return cached[1]

    data = _fetch_lhm_json(url)
    if data is None:
        print("[warn] LHM 读取失败 (web server 没开 / 防火墙?)", file=sys.stderr)
        res = {"cpu_load": None, "cpu_temp": None, "ram": None}
    else:
        res = _parse_lhm(data)
    _lhm_cache[url] = (now, res)
    return res


# ----------------------------------------------------------------------------
# 出图
# ----------------------------------------------------------------------------
def load_font(size: int) -> ImageFont.FreeTypeFont | ImageFont.ImageFont:
    for path in FONT_CANDIDATES:
        try:
            return ImageFont.truetype(path, size)
        except OSError:
            continue
    print("[warn] 找不到 TTF 字体, 降级到 PIL 内置位图字体 (会很小)", file=sys.stderr)
    return ImageFont.load_default()


def fmt(v: float | None, unit: str = "%") -> str:
    return "N/A" if v is None else f"{v:.0f}{unit}"


def tfmt(v: float | None) -> str:
    return "N/A" if v is None else f"{v:.0f}°C"


def render_message(text: str) -> bytes:
    """居中大字消息屏 (欢迎/告别/自定义), 自动按宽度折行."""
    img = Image.new("L", (WIDTH, HEIGHT), color=255)
    draw = ImageDraw.Draw(img)
    _draw_wrapped_centered(draw, text, load_font(72), margin=80)
    return _to_png(img)


# ----------------------------------------------------------------------------
# 仪表盘布局几何 (见预览 v2: 上 CPU / 下 GPU, 各顶部三列数字 + 下方镜像 sweep 图)
# ----------------------------------------------------------------------------
F_VAL, F_LAB = 104, 44          # 唯一两级字号: 大数字 / 小标签
MARGIN = 36
COL_W = 4                       # sweep 每次新增的竖条宽度 (px)
COL_CENTERS = [WIDTH * 1 // 6, WIDTH * 3 // 6, WIDTH * 5 // 6]
NUM_TOP = 18                    # 大数字顶 (相对 block y0)
NUM_BAND = (6, 136)            # 数字带 (相对 y0), 局部刷新范围
LAB_Y = 140                    # 标签 y (相对 y0)
CHART_TOP, CHART_BOT = 205, 706  # 图表上下沿 (相对 y0)
CHART_X0, CHART_X1 = MARGIN, WIDTH - MARGIN
BLOCKS = [  # (key, y0, 首列标签, 内存标签)
    ("cpu", 20, "CPU", "RAM"),
    ("gpu", 744, "GPU", "VRAM"),
]


class Dashboard:
    """状态化仪表盘: 在 WSL 维持一份 framebuffer, 每 tick 只局部刷新变化的小块.

    - sweep 图: 每 tick 在图表右侧画一条新竖条, 只 du 局部刷那一列;
      画满整宽后清空图表、从左重来.
    - 数字: 仅在文字变化时才重画+局部刷该块数字带.
    - 周期性 (round 0 / 每 flush_every 轮) gc16 整屏全刷, 洗掉 du 残影.
    """

    def __init__(self, host: str):
        self.host = host
        self.fb = Image.new("L", (WIDTH, HEIGHT), color=255)
        self.draw = ImageDraw.Draw(self.fb)
        self.f_val = load_font(F_VAL)
        self.f_lab = load_font(F_LAB)
        self.sweep_x = CHART_X0
        self.last: dict[str, tuple | None] = {"cpu": None, "gpu": None}
        self._draw_static()

    def _chart_rect(self, y0: int) -> tuple[int, int, int, int]:
        return CHART_X0, y0 + CHART_TOP, CHART_X1, y0 + CHART_BOT

    def _draw_static(self) -> None:
        """静态元素 (只画一次, 永不局部刷): 标签 + 图表边框."""
        for _key, y0, lab0, memlab in BLOCKS:
            for cx, lab in zip(COL_CENTERS, [lab0, "TEMP", memlab]):
                self.draw.text((cx, y0 + LAB_Y), lab, font=self.f_lab,
                               fill=0, anchor="ma")
            x0, cy0, x1, cy1 = self._chart_rect(y0)
            self.draw.rectangle([x0, cy0, x1, cy1], outline=0, width=2)

    @staticmethod
    def _num_strings(key: str, m: dict) -> tuple[str, str, str]:
        if key == "cpu":
            return fmt(m["cpu"]), tfmt(m["cpu_temp"]), fmt(m["mem"])
        return fmt(m["gpu"]), tfmt(m["gpu_temp"]), fmt(m["vram"])

    def _draw_numbers(self, y0: int, strs: tuple[str, str, str]) -> tuple:
        """白底擦掉数字带, 重画三列数字. 返回需局部刷新的区域."""
        by0, by1 = y0 + NUM_BAND[0], y0 + NUM_BAND[1]
        self.draw.rectangle([0, by0, WIDTH, by1], fill=255)
        for cx, s in zip(COL_CENTERS, strs):
            self.draw.text((cx, y0 + NUM_TOP), s, font=self.f_val,
                           fill=0, anchor="ma")
        return (0, by0, WIDTH, by1)

    def _draw_column(self, y0: int, value: float | None) -> tuple:
        """在 sweep_x 处画一条镜像对称竖条. 返回需局部刷新的整高细条区域."""
        x0, cy0, x1, cy1 = self._chart_rect(y0)
        cyc = (cy0 + cy1) // 2
        half = (cy1 - cy0) // 2 - 3
        x = self.sweep_x
        if value is not None:
            h = int(half * max(0.0, min(100.0, value)) / 100.0)
            if h > 0:
                self.draw.rectangle([x, cyc - h, x + COL_W - 1, cyc + h], fill=0)
        return (x, cy0 + 2, x + COL_W, cy1 - 1)

    def _clear_charts(self) -> None:
        """清空两块图表内部 (保留边框), 用于 sweep 画满后从左重来."""
        for _key, y0, *_ in BLOCKS:
            x0, cy0, x1, cy1 = self._chart_rect(y0)
            self.draw.rectangle([x0 + 2, cy0 + 2, x1 - 2, cy1 - 2], fill=255)

    def compose(self, m: dict) -> tuple[list, list, bool]:
        """把本 tick 的变化画进 framebuffer. 返回 (列区域, 数字区域, 是否换行)."""
        wrapped = self.sweep_x + COL_W > CHART_X1
        if wrapped:
            self._clear_charts()
            self.sweep_x = CHART_X0
        col_regions, num_regions = [], []
        for key, y0, *_ in BLOCKS:
            val = m["cpu"] if key == "cpu" else m["gpu"]
            col_regions.append(self._draw_column(y0, val))
            strs = self._num_strings(key, m)
            if strs != self.last[key]:
                num_regions.append(self._draw_numbers(y0, strs))
                self.last[key] = strs
        self.sweep_x += COL_W
        return col_regions, num_regions, wrapped

    # ---- 推送 ----
    def push_region(self, region: tuple, waveform: str) -> None:
        x0, y0, x1, y1 = region
        buf = io.BytesIO()
        self.fb.crop((x0, y0, x1, y1)).save(buf, format="PNG")
        cmd = (f"cat > {REMOTE_PNG} && {EIPS} -g {REMOTE_PNG} "
               f"-x {x0} -y {y0} -w {waveform}")
        proc = subprocess.run(["ssh", self.host, cmd], input=buf.getvalue(),
                              capture_output=True, timeout=30)
        if proc.returncode != 0:
            raise RuntimeError(
                f"局部刷新失败 (rc={proc.returncode}): "
                f"{proc.stderr.decode(errors='replace').strip()}")

    def full_refresh(self, waveform: str = "gc16", clear: bool = False) -> None:
        buf = io.BytesIO()
        self.fb.save(buf, format="PNG")
        push(buf.getvalue(), self.host, waveform=waveform,
             full=(waveform == "gc16"), clear=clear)

    def save(self, path: str) -> None:
        self.fb.save(path)


def _draw_wrapped_centered(draw, text, font, margin) -> None:
    max_w = WIDTH - 2 * margin
    lines: list[str] = []
    for paragraph in text.split("\n"):
        words = paragraph.split(" ")
        cur = ""
        for w in words:
            trial = (cur + " " + w).strip()
            if draw.textlength(trial, font=font) <= max_w or not cur:
                cur = trial
            else:
                lines.append(cur)
                cur = w
        lines.append(cur)
    # 行高
    ascent, descent = font.getmetrics()
    lh = int((ascent + descent) * 1.3)
    total_h = lh * len(lines)
    y = (HEIGHT - total_h) // 2
    for line in lines:
        w = draw.textlength(line, font=font)
        draw.text(((WIDTH - w) // 2, y), line, font=font, fill=0)
        y += lh


def _to_png(img: Image.Image) -> bytes:
    buf = io.BytesIO()
    img.save(buf, format="PNG")  # L 模式 -> 8-bit 灰度 PNG
    return buf.getvalue()


# ----------------------------------------------------------------------------
# 推送 + 刷新
# ----------------------------------------------------------------------------
def push(png: bytes, host: str, waveform: str, full: bool, clear: bool,
         timeout: float = 60) -> None:
    """管道法: 一次 SSH 连接同时完成传输 + eips 刷新.

    waveform: eips -w 波形. du = 快速灰度 (~260-450ms, 会累积残影);
              gc16 = 16 级灰阶全刷 (~600ms-1s, 干净但慢, 会闪).
    full:     加 -f 强制全刷 (洗白残影). 通常和 gc16 搭配做周期性洗白.
    clear:    刷新前先 eips -c 整屏洗白.
    """
    eips_flags = f"-g {REMOTE_PNG} -w {waveform}"
    if full:
        eips_flags += " -f"
    remote_cmd = f"cat > {REMOTE_PNG}"
    if clear:
        remote_cmd += f" && {EIPS} -c"
    remote_cmd += f" && {EIPS} {eips_flags}"

    proc = subprocess.run(
        ["ssh", host, remote_cmd],
        input=png,
        capture_output=True,
        timeout=timeout,
    )
    if proc.returncode != 0:
        raise RuntimeError(
            f"ssh push 失败 (rc={proc.returncode}): "
            f"{proc.stderr.decode(errors='replace').strip()}"
        )


def push_screen(args, lines: list[str], timeout: float = 60) -> None:
    """推一屏文字 (欢迎/告别): gc16 全刷, 干净不留残影."""
    png = render_message("\n".join(lines))
    push(png, args.host, waveform="gc16", full=True, clear=True, timeout=timeout)


# ----------------------------------------------------------------------------
# 数据采集 + 主循环
# ----------------------------------------------------------------------------
def collect_metrics(args) -> dict:
    # CPU 占用/温度 + 内存: 优先 LHM (整机真实视角); GPU: nvidia-smi (真显卡).
    lhm = read_lhm(args.lhm_url, cache_ttl=args.temp_interval)
    gpu, vram, gpu_temp = read_gpu()
    cpu, mem = lhm["cpu_load"], lhm["ram"]
    # LHM 拿不到时降级到 psutil (注意: 只是 WSL 子系统视角, 不准, 仅兜底)
    if cpu is None or mem is None:
        p_cpu, p_mem = read_cpu_mem(sample=args.cpu_sample)
        cpu = p_cpu if cpu is None else cpu
        mem = p_mem if mem is None else mem
    return {
        "cpu": cpu, "mem": mem, "gpu": gpu, "vram": vram,
        "cpu_temp": lhm["cpu_temp"], "gpu_temp": gpu_temp,
    }


def _print_metrics(m: dict) -> None:
    print(
        f"CPU={fmt(m['cpu'])}/{tfmt(m['cpu_temp'])} RAM={fmt(m['mem'])}  "
        f"GPU={fmt(m['gpu'])}/{tfmt(m['gpu_temp'])} VRAM={fmt(m['vram'])}"
    )


def tick(dash: Dashboard, args, round_idx: int) -> None:
    """采集 → 画进 framebuffer → 局部刷新 (或周期/换行时整刷)."""
    t0 = time.perf_counter()
    m = collect_metrics(args)
    t1 = time.perf_counter()

    col_regions, num_regions, wrapped = dash.compose(m)
    t2 = time.perf_counter()

    # round 0 / 每 flush_every 轮 / --full → gc16 整屏全刷洗白; 其余局部 du 快刷.
    do_flush = args.full or round_idx == 0 or (
        args.flush_every > 0 and round_idx % args.flush_every == 0)
    if do_flush:
        dash.full_refresh(waveform="gc16", clear=args.clear)
        tag = "gc16 整屏"
    elif wrapped:
        # 图表画满, 已清空; 整块图表区重刷一次, 数字带照常局部刷.
        for _key, y0, *_ in BLOCKS:
            dash.push_region(dash._chart_rect(y0), "gc16")
        for r in num_regions:
            dash.push_region(r, args.waveform)
        tag = f"换行重画 + {len(num_regions)} 数字块"
    else:
        for r in col_regions:
            dash.push_region(r, args.waveform)
        for r in num_regions:
            dash.push_region(r, args.waveform)
        tag = f"{len(col_regions)} 列 + {len(num_regions)} 数字块 [{args.waveform}]"
    t3 = time.perf_counter()

    _print_metrics(m)
    print(f"  耗时: 采集 {t1 - t0:.2f}s | 画 {t2 - t1:.2f}s | "
          f"刷 {t3 - t2:.2f}s ({tag}) | 总 {t3 - t0:.2f}s")


def main() -> int:
    ap = argparse.ArgumentParser(description="Kindle PW3 dashboard pusher")
    ap.add_argument("--host", default="kindle", help="SSH 别名/主机 (默认 kindle)")
    ap.add_argument(
        "--interval", type=float, default=10, help="循环间隔秒 (默认 10, 0=不等待)"
    )
    ap.add_argument(
        "--cpu-sample",
        type=float,
        default=1.0,
        help="psutil CPU 采样窗口秒 (默认 1.0; 0=非阻塞, 极限测速用)",
    )
    ap.add_argument(
        "--lhm-url",
        default=None,
        help="LibreHardwareMonitor web server 地址 (取 CPU 温度). "
        "默认自动用 WSL 网关 IP 直连; localhost 则走 powershell",
    )
    ap.add_argument(
        "--temp-interval",
        type=float,
        default=5.0,
        help="CPU 温度缓存秒数 (默认 5; 避免每轮都掏一次 powershell)",
    )
    ap.add_argument("--once", action="store_true", help="只跑一次")
    ap.add_argument("--clear", action="store_true", help="刷新前先 eips -c 洗白")
    ap.add_argument("--full", action="store_true", help="每轮都 gc16 -f 强制全刷")
    ap.add_argument(
        "--waveform",
        default="du",
        help="快刷波形 (du/gl16/a2/reagl 等, 默认 du). 越快残影越多",
    )
    ap.add_argument(
        "--flush-every",
        type=int,
        default=10,
        help="每 N 轮做一次 gc16 全刷洗白 (0 = 从不, 第 0 轮总会全刷). 默认 10",
    )
    ap.add_argument("--message", help="推一条自定义文字消息 (而非系统监控)")
    ap.add_argument("--save", help="同时把 PNG 存到本地路径")
    ap.add_argument(
        "--welcome-secs",
        type=float,
        default=10.0,
        help="循环开始时显示欢迎屏的秒数 (默认 10; 0=跳过)",
    )
    ap.add_argument(
        "--no-farewell",
        action="store_true",
        help="退出时不显示告别屏",
    )
    args = ap.parse_args()

    if args.lhm_url is None:
        args.lhm_url = default_lhm_url()

    # 自定义消息: 一次性全屏推送
    if args.message is not None:
        png = render_message(args.message)
        if args.save:
            with open(args.save, "wb") as f:
                f.write(png)
        push(png, args.host, waveform="gc16", full=True, clear=args.clear)
        return 0

    # 单次: 画一帧仪表盘 (含首列), gc16 整刷
    if args.once:
        dash = Dashboard(args.host)
        m = collect_metrics(args)
        dash.compose(m)
        if args.save:
            dash.save(args.save)
        dash.full_refresh(waveform="gc16", clear=args.clear)
        _print_metrics(m)
        return 0

    # ---- 循环 (常驻) 模式 ----
    if not acquire_lock():
        print(f"[error] 已有实例在运行 (见 {PIDFILE}), 退出", file=sys.stderr)
        return 1

    stop = {"flag": False}

    def on_signal(signum, frame):  # noqa: ARG001
        stop["flag"] = True

    signal.signal(signal.SIGTERM, on_signal)
    signal.signal(signal.SIGINT, on_signal)

    print(
        f"常驻模式: 每 {args.interval}s 推送到 {args.host}, sweep 局部刷 "
        f"({args.waveform}), 每 {args.flush_every} 轮 gc16 整刷 "
        f"(SIGTERM/Ctrl-C 优雅退出)"
    )

    # 欢迎屏
    if args.welcome_secs > 0:
        try:
            push_screen(args, WELCOME_LINES, timeout=15)
        except Exception as e:  # noqa: BLE001
            print(f"[warn] 欢迎屏失败: {e}", file=sys.stderr)
        _interruptible_sleep(args.welcome_secs, stop)

    dash = Dashboard(args.host)
    round_idx = 0
    while not stop["flag"]:
        try:
            tick(dash, args, round_idx)
        except Exception as e:  # noqa: BLE001
            print(f"[error] 本轮失败, 继续: {e}", file=sys.stderr)
        round_idx += 1
        _interruptible_sleep(args.interval, stop)

    # 告别屏 (手动 stop 必现; 关机时 best-effort, 短超时免拖死关机)
    if not args.no_farewell:
        try:
            push_screen(args, FAREWELL_LINES, timeout=8)
        except Exception as e:  # noqa: BLE001
            print(f"[warn] 告别屏失败: {e}", file=sys.stderr)
    print("已优雅退出")
    return 0


def _interruptible_sleep(secs: float, stop: dict) -> None:
    """分片 sleep, 收到停止信号立即返回 (告别屏更及时)."""
    end = time.monotonic() + secs
    while not stop["flag"] and time.monotonic() < end:
        time.sleep(min(0.2, max(0.0, end - time.monotonic())))


if __name__ == "__main__":
    sys.exit(main())
