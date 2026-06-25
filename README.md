# 61

<p align="center">
  😄 个人自用视频站 😄
</p>

<p align="center">
  <a href="#快速开始">快速开始</a> ·
  <a href="#功能特性">功能特性</a> ·
  <a href="#数据存放位置">数据目录</a> ·
  <a href="#许可证">许可证</a>
</p>

---

## 功能特性

### 媒体管理
- **多存储后端** — 兼容多家网盘和本地存储，自动按文件类型路由
- **封面 & 预览** — 自动生成封面图和短视频片段，首页快速选片
- **多类型统一** — 视频 / 音频 / 图集 / 文档 / 小说 都在同一个媒体库里

### 内容浏览
- **音视频分离** — 列表页可以只看音频或只看视频
- **图集模式** — 漫画 / 套图专用视图，支持翻页阅读
- **小说阅读器** — 支持纯文本和 PDF，记住阅读位置
- **PDF 阅读器** — 分页导航、缩放、文字可选复制
- **动漫解析** — 粘贴 B 站链接直接播放（其他站点通用兜底）
- **短视频模式** — 一键切换抖音风格，沉浸刷片

### 检索与管理
- **全局搜索** — 顶部搜索栏快速定位
- **资源中心** — 接入外部资源搜索，一键导入到网盘
- **系统标签** — 固定标签池自动分类
- **可视化后台** — 爬虫 / 存储 / 活跃统计一目了然

### 自定义体验
- **多主题切换** — 亮 / 暗 / 护眼 / 复古绿
- **阅读偏好** — 字号 / 行距 / 字体 / 宽度可调
- **继续观看** — 首页接着上次播放的位置看
- **错误保护** — 任意子组件崩溃都有降级 UI，不白屏
- **加载占位** — 切换页面不再空白闪烁

---

---

## 快速开始

### 方式一：一键安装脚本（推荐）

```bash
sudo apt update && sudo apt install -y curl ca-certificates
curl -fsSL https://raw.githubusercontent.com/iwwater/61video/main/install.sh -o install.sh
sudo bash install.sh
```

部署完成后访问：

| 地址 | 说明 |
|------|------|
| `http://服务器IP:6191/` | 前台 |
| `http://服务器IP:6191/admin` | 后台管理 |

**注意：如果首次访问，显示502，可以运行 `61 restart` 重启一下服务**

安装后自动注册 `61` 管理命令：

```bash
61            # 打开管理菜单
61 status     # 查看运行状态
61 logs       # 查看日志
61 update     # 更新到最新版本
61 restart    # 重启服务
61 stop       # 停止服务
```

> `video-site-61` 为等效别名，两者可互换使用。

**已部署用户升级：**

```bash
61 update
```

升级会保留现有 `config.yaml`、数据库、封面、预览、上传文件和爬虫数据。脚本会自动安装或检查 `ffmpeg` / `ffprobe` 等运行依赖，并在新版本启动失败时回滚到升级前文件。

**自定义端口：**

```bash
FRONTEND_PORT=8080 sudo -E bash install.sh
```

**旧版本升级（v0.0.2 之前）：**

旧版脚本直接执行 `61 update` 可能失败，先执行以下修复命令：

```bash
curl -fsSL https://raw.githubusercontent.com/iwwater/61video/main/install.sh -o /tmp/install-61.sh
sudo bash /tmp/install-61.sh update
```

---

### 方式二：Docker Compose 部署

**1. 准备目录**

```bash
mkdir -p video-site-61 && cd video-site-61
```

**2. 创建 `docker-compose.yml`**

```yaml
services:
  video-site-61:
    image: ghcr.io/iwwater/61video:stable
    container_name: video-site-61
    ports:
      - "6191:6191"
    volumes:
      - ./data:/opt/video-site-61/data
    restart: unless-stopped
```
创建yml文件后运行下面指令
```bash
docker compose pull
docker compose up -d
```

如果想固定某个 Release 版本，可以改成明确的 tag，例如：

```yaml
image: ghcr.io/iwwater/61video:v0.0.6
```

或直接拉取仓库内置配置：

```bash
curl -fsSL https://raw.githubusercontent.com/iwwater/61video/main/docker-compose.yml -o docker-compose.yml
```

**3. 启动**

```bash
docker compose up -d
```

**常用命令：**

```bash
docker compose logs -f       # 查看日志
docker compose pull          # 拉取最新正式版 stable 镜像
docker compose up -d         # 更新并重启
```

> 所有配置、数据库、封面、预览及上传文件均保存在 `./data/` 目录下。
> 从旧版本升级 Docker 部署时，执行 `docker compose pull && docker compose up -d` 即可；`./data/` 不会被镜像更新覆盖。

---

## 数据存放位置

### 一键脚本部署

| 路径 | 内容 |
|------|------|
| `/opt/video-site-61/config.yaml` | 配置文件、管理员账号、网盘凭证 |
| `/opt/video-site-61/data/video-site.db` | SQLite 数据库 |
| `/opt/video-site-61/data/previews/` | 封面图和预览片段 |

### Docker Compose 部署

| 路径 | 内容 |
|------|------|
| `./data/config.yaml` | 配置文件、管理员账号、网盘凭证 |
| `./data/video-site.db` | SQLite 数据库 |
| `./data/previews/` | 封面图和预览片段 |
| `./data/uploads/` | 本地上传的视频文件 |
| `./data/spider61/` | 61 爬虫抓取的视频文件 |

---

## 使用须知

本项目面向**个人私有部署**，请仅接入你有权访问和管理的内容，并遵守对应网盘、站点的服务条款及所在地法律法规。

> 不对外传播，仅限个人使用。

---

---

## 许可证

本项目基于 [MIT License](LICENSE) 开源。

---