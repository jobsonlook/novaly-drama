# Docker 本地部署指南

本方案将 Novaly、doubao-web-api、Chromium、FFmpeg 和浏览器登录桌面放进同一个容器。不需要 COS/TOS、外部数据库或宿主机 Chrome；AI 仍通过联网服务生成。

> 目前提供源码构建方案，尚未发布预构建容器镜像。已通过 Compose 配置检查、前端生产构建、两个 Go 服务的回归测试及 Linux ARM64 编译。本次本地镜像构建因基础镜像下载过慢未能完成；容器启动、浏览器登录和完整生成流程仍待验证，不要把它当作生产环境可用性承诺。账号登录与生成额度取决于豆包网页。macOS / Windows 使用 Docker Desktop 的 Linux 容器模式，Linux 使用 Docker Engine + Compose 插件。

[返回 README](../README.md)

## 1. 下载与启动

先安装并启动 Docker，确认以下命令可用：

```bash
docker version
docker compose version
```

下载代码并启动：

```bash
git clone https://github.com/jobsonlook/novaly-drama.git
cd novaly-drama
docker compose up -d --build
docker compose ps
docker compose logs --tail=100 novaly
```

首次需下载 Node、Go、Debian 基础镜像、npm / Go 依赖和 Chromium。请预留数 GB 磁盘空间，以及浏览器和视频处理所需内存；实际占用随项目增长。镜像体积会明显大于原生运行包。

如果已经启动原生版，请等待生成结束并正常停止原生 Novaly 和豆包服务，避免端口冲突。不要同时用两个服务写同一套数据库。

## 2. 打开工作台并登录豆包

| 地址 | 用途 |
| --- | --- |
| http://127.0.0.1:8085 | Novaly 工作台 |
| http://127.0.0.1:8086/admin | 豆包账号管理，需先启动豆包服务 |
| http://127.0.0.1:6080/vnc.html | noVNC 桌面，用来操作容器里的 Chromium |

1. 在工作台「设置中心」点击「启动 doubao-web-api」。第一次启动可能需要等待浏览器就绪。
2. 在项目根目录执行以下命令，读取本次启动的桌面密码：

   ```bash
   docker compose exec novaly cat /tmp/novaly-vnc-password
   ```

3. 打开 `http://127.0.0.1:6080/vnc.html`，点击连接，输入上一步密码。
4. 在桌面里的 Chromium 登录自己的豆包账号。按网页提示完成扫码或验证。
5. 返回 Novaly，确认服务运行并选择 `Seedream Web` / `Seedance Web` 等账号支持的模型，先测试一个短镜头。

**本机 Chrome 已登录，不代表容器里也登录。** noVNC 提供的是容器桌面；它不是图片生成服务。刚开容器还没启动豆包时，桌面可能只有空背景，这是正常的。

桌面密码在每次容器启动时随机生成，不打印到日志，不提交仓库。豆包浏览器状态保存在独立数据卷，重启后通常保留，但平台可能要求重新验证。不要把密码、登录二维码或会话目录贴到公开 Issue。

「healthy」只表示 Novaly 的健康接口正常，不代表已经登录豆包，也不代表账号有额度。写作、自动拆解、语音和其他服务商仍需单独配置自己的凭据。

## 3. 日常使用与日志

```bash
# 启动已有容器
docker compose up -d

# 查看 Novaly 和桌面进程日志
docker compose logs -f --tail=100 novaly

# 查看豆包日志（先在工作台启动过豆包服务）
docker compose exec novaly tail -f /app/doubao-web-api/data/service.log

# 等待生成完成后，停止全部服务
docker compose stop
```

Novaly 日志同时保存在 `/app/backend/data/novaly.log`，支持工作台的日志查看入口。日志可能包含提示词等项目内容，请勿直接公开。

查看日志时按 Ctrl+C 只结束日志跟随。请先等待正在生成的任务完成再停止容器；中断可能使任务失败。

## 4. 数据在哪里

Compose 创建三个命名数据卷，实际名称一般带项目名前缀。容器删除后仍可保留数据；命名卷的工作方式参见 [Docker Compose 服务配置](https://docs.docker.com/reference/compose-file/services/)。

| Compose 卷名 | 容器路径 | 内容 |
| --- | --- | --- |
| `novaly-data` | `/app/backend/data` | SQLite、项目、剧本、分镜、图片、视频、配音相关文件 |
| `doubao-data` | `/app/doubao-web-api/data` | 豆包账号管理、任务记录、服务日志 |
| `doubao-session` | `/app/doubao-web-api/session` | Chromium 登录状态与浏览器资料 |

`docker compose stop` 和 `docker compose down` 不会删除上述命名卷。**不要执行 `docker compose down -v`，除非明确要删除创作数据和登录状态。** 切换项目目录名称或 `-p` 参数可能创建另一套卷，表现为“项目不见了”，此时不要删除旧卷。

镜像构建排除了原生版数据库、登录目录、密钥、日志和备份；构建镜像不会把本机创作数据打进去。README 中少量展示图片也不进入镜像。

## 5. 备份和恢复

建议同时保留目录结构与素材文件，不要只备份数据库。下面命令在项目根目录执行；备份前先等待任务完成。

```bash
docker compose stop
mkdir backup
docker compose cp novaly:/app/backend/data backup/novaly-data
docker compose cp novaly:/app/doubao-web-api/data backup/doubao-data
docker compose cp novaly:/app/doubao-web-api/session backup/doubao-session
docker compose start
```

每次使用一个新的备份目录，避免覆盖已有备份或把旧文件混进去。该备份包含 API Key 和登录资料的可能性很高，请保存在安全位置，**不要提交 Git**。

恢复到已经创建、但停止运行的容器（会覆盖同名文件，请先备份目标数据）：

```bash
docker compose stop
docker compose cp backup/novaly-data/. novaly:/app/backend/data/
docker compose cp backup/doubao-data/. novaly:/app/doubao-web-api/data/
docker compose cp backup/doubao-session/. novaly:/app/doubao-web-api/session/
# cp 可能改变所有者，在启动前修复卷权限：
docker compose run --rm --no-deps --user root --cap-add CHOWN --cap-add DAC_OVERRIDE --entrypoint chown novaly -R 1000:1000 /app/backend/data /app/doubao-web-api/data /app/doubao-web-api/session
docker compose up -d
```

如果还没有容器，先运行 `docker compose create`。默认容器使用 UID 1000 的普通用户；上面的 root 命令只用于手动恢复后的所有者修复，日常服务仍以普通用户运行。跨系统迁移 Chrome 登录目录可能失效或包含不兼容路径，需要重新登录。

**从原生版迁移：** 停止原生服务，备份后把原生 `backend/data/` 复制到容器 `/app/backend/data/`（按上面的 `compose cp` 方式），再修复权限。建议豆包账号在容器里重新登录，避免直接复用另一系统的 Chrome profile。仅复制项目数据不会替你迁移账号状态。

## 6. 更新

先备份，再执行：

```bash
docker compose stop
git pull --ff-only
docker compose up -d --build
```

数据卷保留。不要直接把更老版本的程序连接到升级后的数据库；要回退时，应同时恢复对应版本的备份。

## 7. 常见问题

| 问题 | 处理方法 |
| --- | --- |
| 构建卡在 `load metadata` / 拉镜像 | 检查 Docker 的网络或代理，确认能访问镜像仓库；浏览器能上网不代表 Docker 引擎也能访问 |
| npm / Go 下载失败 | 查看报错的依赖源，修复 Docker 网络后重新构建，已完成层会尽量复用 |
| `port is already allocated` | 先停止占用 8085、8086 或 6080 的旧实例；不要随意杀掉未知进程 |
| 8085 正常，8086 打不开 | 在设置中心启动豆包服务；其 HTTP 端口不是容器启动时自动开启 |
| noVNC 连接失败 | 检查 `docker compose ps` 和容器日志，确认访问 6080，重启后重新读取密码 |
| 桌面没有 Chrome | 先启动豆包服务；失败时查看 `service.log` |
| 中文显示异常 | 镜像包含 Noto CJK 字体；确认实际使用的是最新构建镜像 |
| 浏览器崩溃、视频处理被终止 | 检查 Docker 内存和磁盘，保留 Compose 中的 `shm_size: 1gb`；减少同时运行的任务 |
| 上传或 SQLite 报只读 / 权限不足 | 检查卷所有者；若手动恢复过文件，按恢复步骤修复 UID 1000 的权限 |
| 容器重启后登录失效 | 确认使用原来的 `doubao-session` 卷，并在容器桌面重新登录 |
| 提示额度不足 / 验证 / 风控 | 在豆包网页处理账号提示；容器不会增加额度或绕过限制 |

本项目本地服务管理固定使用 8086，不建议只改宿主机端口映射或供应商地址，否则页面入口与服务调用地址可能不一致。

## 8. 安全边界与实现

- 默认端口只发布到宿主机 `127.0.0.1`，用于单机使用。不要改成 `0.0.0.0` 公开发布；工作台、管理接口和浏览器会话不是多用户公网服务。
- noVNC 有单独密码，但这个密码不保护 Novaly / 豆包管理接口。浏览器桌面方案使用 [noVNC](https://github.com/novnc/noVNC)，原始 VNC 端口 5900 和 Chrome 调试端口不向宿主机发布。
- 运行容器不使用 privileged，不挂载 Docker socket，以普通用户运行并删除默认 capabilities。Linux Chromium 启动逻辑使用 `--no-sandbox`，容器隔离不能替代所有浏览器安全机制，因此只操作可信的豆包页面，不把桌面当通用浏览器。
- Docker 内 `NOVALY_LISTEN_HOST=0.0.0.0` 用于允许宿主机端口转发；原生启动仍默认监听 `127.0.0.1`，不会因此自动对外开放。
- 本方案没有本地大模型推理，也不需要为 AI 生成配置本地 GPU。提示词、参考图片等仍会发送到选择的云端服务。
