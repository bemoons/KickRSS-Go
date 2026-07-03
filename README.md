# KickRSS-Go - 智能自进化 PWA RSS 阅读器 (Go极速版)

**KickRSS-Go** 是 KickRSS 的 Go 语言移植版本。它保留了原版的所有核心特性：AI 智能分桶分类、AI 自动摘要与对照翻译、个人阅读画像可视化，并采用了极具科技感的赛博朋克玻璃拟物化视觉设计。

得益于 Go 语言的极高运行效率与极低资源开销，此版本非常适合在轻量级云服务器、树莓派等资源受限的设备上部署运行。

---

## 🚀 快速开始

### 方式一：使用 Docker Compose / Portainer Stack 部署（推荐 ⭐）

我们提供了预编译托管镜像，可实现开箱即用、一键拉取部署，无需本地安装 Golang 环境或手动编译。

1. **创建配置文件和挂载目录**：
   在宿主机上创建数据持久化目录（例如 `/home/bemoon/kickRSS/data` 或您自定义的路径）：
   ```bash
   mkdir -p /home/bemoon/kickRSS/data
   ```

2. **编写 docker-compose.yml**：
   在部署目录（或 Portainer Stack 编辑器）中写入以下配置：
   ```yaml
   version: '3.8'
   services:
     kickrss-go:
       image: ghcr.io/bemoons/kickrss-go:latest  # 直接使用云端自动构建的预编译镜像
       container_name: kickrss-go
       network_mode: host
       restart: unless-stopped
       volumes:
         - /home/bemoon/kickRSS/data:/app/data
       environment:
         - PORT=8888
         - TZ=Asia/Shanghai
   ```

3. **启动容器**：
   直接运行以下命令（或在 Portainer 界面点击 Deploy Stack）：
   ```bash
   docker compose up -d
   ```
   *首次启动时，系统会自动在挂载的 `data` 目录下初始化生成 `config.yaml` 配置文件及 `myrss.db` 数据库。*

4. **修改配置**：
   编辑挂载目录下的 `config.yaml` 配置文件，填入您的 OpenAI 兼容接口地址与 API Key，然后重启容器即可生效：
   ```bash
   docker compose restart
   ```

5. **访问服务**：
   在浏览器中打开 `http://<您的服务器IP>:8888` 即可开始使用。

---

### 方式二：直接运行预编译二进制包 (无 Docker 环境)

如果您不想使用 Docker，可以直接在 Linux x86_64 宿主机上运行我们编译好的轻量包。

1. **下载并解压包**：
   ```bash
   tar -xzf kickrss-go-linux-amd64.tar.gz
   cd release_package
   ```

2. **配置与运行**：
   * 首次运行 `./kickrss`，会自动生成 `config.yaml`。
   * 编辑 `config.yaml` 填写大模型接口信息。
   * 再次启动：`./kickrss` 即可。

---

## ⚙️ 配置文件说明 (`config.yaml`)

核心 AI 及判定选项可以在 `./data/config.yaml` 中进行如下调整：

```yaml
# 数据库路径 (Docker 下请保持为 data/myrss.db 以持久化)
db_path: data/myrss.db

# 服务监听端口
port: 8888

# 订阅源自动抓取周期 (分钟)
fetch_interval_minutes: 15

# 🤖 AI 大模型接口配置
ai:
  default:
    base_url: https://api.openai.com/v1   # API 端点地址
    api_key: your-api-key-here           # 您的 API Key
    model: gpt-4o-mini                    # 使用的 AI 模型名称
  pregenerate: false                      # 是否在后台自动预生成摘要
  stream: true                            # 是否流式输出
  auto_summary: true                      # 点击文章时是否自动开始生成摘要
  summary_language: zh                    # 摘要的目标语言

# 🧠 全文判定与分类阈值
fulltext:
  min_text_chars: 200                     # 判定为全文的最小字符长度
```
