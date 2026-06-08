# ============================================================================
# Kindle Dashboard 机器相关配置 —— 部署到新机器时只改这一个文件
# 被 kindlectl / install.sh 引用 (source)
# ============================================================================

# WSL 发行版名 (看 `wsl.exe -l -v` 里的 NAME)
DISTRO="Ubuntu"

# WSL 用户名 (运行脚本的用户, 即 `whoami`)
WSL_USER="sly"

# Windows 用户名 (用于定位「启动」文件夹做开机自启; 看 C:\Users\ 下的目录名)
WIN_USER="slyth"

# Kindle 的 IP (install.sh 会写进 ~/.ssh/config 的 `kindle` 别名)
KINDLE_IP="10.0.0.43"

# 项目所在目录 (WSL 路径). 默认取本文件所在目录, 一般不用改.
PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# 传给 worker 的参数:
#   --interval 10  每 10s 刷新
#   --cpu-sample 0 非阻塞采样 (CPU 占用其实走 LHM, 这只是兜底用的 psutil 模式)
DASH_ARGS="--interval 10 --cpu-sample 0"
