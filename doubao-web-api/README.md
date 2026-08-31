# doubao-web-api

豆包网页 API 逆向代理服务（Go），通过 CDP 连接 Chrome，在浏览器上下文中调用豆包 Samantha 接口，对外暴露与火山引擎 Ark SDK 兼容的 HTTP API。

## 功能

- **文生图**：`POST /api/v3/images/generations`（兼容 `arkruntime.GenerateImages`）
- **图文生视频**：`POST /api/v3/contents/generations/tasks`（兼容 `arkruntime.CreateContentGenerationTask` / Seedance）
- CDP 模式：复用已登录的 Chrome 会话，绕过反爬签名（参考 [openclaw-zero-token](https://github.com) 豆包实现）

## 前置条件

1. 默认直接启动 Go 服务即可；若 `9222` 上没有可用的 Chrome，服务会自动执行
   `scripts/start-chrome.sh`（用户数据保存在项目 `session/` 目录，登录状态会持久化）：

```bash
go run ./cmd/server
```

若设置 `DOUBAO_AUTO_RESTART_CHROME=0`，则需要先手动启动：

```bash
./scripts/start-chrome.sh
go run ./cmd/server
```

也可手动指定 Chrome：

```bash
mkdir -p session
/Applications/Google\ Chrome.app/Contents/MacOS/Google\ Chrome \
  --remote-debugging-port=9222 \
  --user-data-dir="$(pwd)/session" \
  "https://www.doubao.com/chat/"
```

2. 在 Chrome 中打开并登录 https://www.doubao.com/chat/

### 切换豆包账号

推荐使用管理后台（SQLite 本地记账）：

1. 打开 http://127.0.0.1:8080/admin
2. 新增账号，填写名称与 Chrome `session_dir`（如 `./session/ba`、`./session/ma`）
3. 每个账号需在对应 Chrome 窗口登录一次豆包（按需拉起的 worker 会用各自 `session_dir`）
4. 「选用」会打开/重启对应账号的 Chrome（若该账号不在当前 worker 池里，会把空闲 worker 切到该 session，或在未达并行上限时新开一路；正在生成的 worker 不会被打断）

### 视频并发生成

默认最多 **2** 路 Seedance 并行（`MAX_PARALLEL_VIDEO`，可改）。**平时只启动 1 个 Chrome**；当已有任务在跑、又进来新的视频任务时，才会再拉起额外 Chrome（端口 `9223`、`9224`…），直到达到上限。例如后台有 `ba`、`ma` 各有余量时，第二路任务会自动打开第二个窗口。

Seedance 剩余额度默认 5（Fast / Mini **共享**同一额度）。视频生成**成功**后按模型扣费：Mini 扣 1、Fast 扣 2；若只剩 1，Fast 仍可生成并扣到 0（一天最多约 3 次 Fast）。某账号网页提示额度用尽时，会把该账号剩余置 0，并自动换另一空闲有额度账号**重试当前任务一次**。额度按**北京时间每天 0 点**自动重置为 5。

注意：每路并行 ≈ 一个完整 Chrome，内存占用明显；账号数不足或额度不足时，实际上限会低于 `MAX_PARALLEL_VIDEO`。

关闭自动启动/重启：`DOUBAO_AUTO_RESTART_CHROME=0`（此时需手动为各端口启动 Chrome；服务退出时也不会自动关 Chrome）。

默认开启时：服务退出（Ctrl+C / SIGTERM）会一并结束占用调试端口的 Chrome。

也可手动改 `.env` 里的 `DOUBAO_SESSION_DIR`（无 `data/active_session` 时生效）。

## 启动服务

```bash
export DOUBAO_CDP_URL=http://127.0.0.1:9222
export PORT=8080
# 可选：API 鉴权
# export DOUBAO_API_KEY=your-secret

go run ./cmd/server
```

管理后台：http://127.0.0.1:8080/admin

## 使用火山引擎 Go SDK 调用

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/volcengine/volcengine-go-sdk/service/arkruntime"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
	"github.com/volcengine/volcengine-go-sdk/volcengine"
)

func main() {
	client := arkruntime.NewClientWithApiKey(
		"dummy-key",
		arkruntime.WithBaseUrl("http://127.0.0.1:8080/api/v3"),
	)

	resp, err := client.GenerateImages(context.Background(), model.GenerateImagesRequest{
		Model:  "doubao-seedream-5-0",
		Prompt: "一只在月球上弹吉他的猫，赛博朋克风格",
		Size:   volcengine.String("1024x1024"),
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, img := range resp.Data {
		fmt.Println(*img.Url)
	}
}
```

## 直接 HTTP 调用

```bash
curl -X POST http://127.0.0.1:8080/api/v3/images/generations \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer dummy-key" \
  -d '{
    "model": "doubao-seedream-5-0",
    "prompt": "一只在月球上弹吉他的猫",
    "size": "1024x1024"
  }'
```

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `DOUBAO_CDP_URL` | `http://127.0.0.1:9222` | 默认/回退 CDP 地址 |
| `DOUBAO_CDP_PORT` | `9222` | Chrome 调试端口基址（worker i 使用 `9222+i`） |
| `DOUBAO_SESSION_DIR` | `./session` | 无账号时的默认 user-data-dir；多账号时各用自己的 `session_dir` |
| `DOUBAO_ACCOUNTS_DB` | `./data/accounts.db` | 多账号 SQLite 路径 |
| `DOUBAO_ACTIVE_SESSION_FILE` | `./data/active_session` | 管理后台「默认账号」session 目录文件 |
| `DOUBAO_CHROME_SCRIPT` | `./scripts/start-chrome.sh` | 自动启动/重启时调用的 Chrome 启动脚本 |
| `DOUBAO_AUTO_RESTART_CHROME` | `1` | `1` 时服务启动/扩容会自动拉起 Chrome；服务退出时关闭 |
| `MAX_PARALLEL_VIDEO` | `2` | 最大并行视频路数；平时只开 1 路，有并发时再按需扩容 |
| `PORT` | `8080` | HTTP 服务端口 |
| `DOUBAO_API_KEY` | 空 | 非空时要求 Bearer Token（`/admin` 可用 `?key=`） |
| `REQUEST_TIMEOUT` | `120` | 文生图超时（秒） |
| `VIDEO_TIMEOUT` | `1200` | 图生视频超时（秒），默认 20 分钟（覆盖 Seedance ~15 分钟 ETA） |
| `VIDEO_UI_MODE` | `skill` | 视频 UI：`skill` 普通视频生成，`office` 办公任务 Turbo |

## 图+文生图（参考图）

### 1. 上传参考图

```bash
curl -X POST http://127.0.0.1:8080/api/v3/images/uploads \
  -F "file=@photo.jpg"
```

返回：

```json
{
  "id": "tos-cn-i-xxx/...",
  "uri": "tos-cn-i-xxx/...",
  "url": "http://127.0.0.1:8080/api/v3/images/proxy?url=...",
  "name": "photo.jpg",
  "format": "jpg"
}
```

### 2. 带参考图生图

使用火山 SDK 的 `image` 字段传入上传得到的 `uri`：

```bash
URI=$(curl -s -X POST http://127.0.0.1:8080/api/v3/images/uploads -F "file=@photo.jpg" | jq -r '.uri')

curl -X POST http://127.0.0.1:8080/api/v3/images/generations \
  -H "Content-Type: application/json" \
  -d "{
    \"model\": \"doubao-seedream-5-0\",
    \"prompt\": \"将这张图片转为水彩画风格\",
    \"image\": \"$URI\",
    \"size\": \"1024x1024\"
  }"
```

也支持 `image` 传 base64 data URI（会自动上传）：

```json
{
  "prompt": "把这张图变成赛博朋克风格",
  "image": "data:image/png;base64,iVBORw0KGgo..."
}
```

## 图+文生视频（Seedance）

接口与火山引擎 Ark SDK 一致，采用异步任务模式：

- 创建任务：`POST /api/v3/contents/generations/tasks`
- 查询任务：`GET /api/v3/contents/generations/tasks/{id}`

### 1. 上传参考图

与图生图相同，先上传参考图获取 `uri`：

```bash
URI=$(curl -s -X POST http://127.0.0.1:8080/api/v3/images/uploads -F "file=@photo.jpg" | jq -r '.uri')
```

### 2. 创建图生视频任务

```bash
TASK_ID=$(curl -s -X POST http://127.0.0.1:8080/api/v3/contents/generations/tasks \
  -H "Content-Type: application/json" \
  -d "{
    \"model\": \"doubao-seedance-2-0-fast\",
    \"content\": [
      {\"type\": \"text\", \"text\": \"让画面中的角色缓缓向前走动，镜头轻微推进\"},
      {\"type\": \"image_url\", \"image_url\": {\"url\": \"$URI\"}, \"role\": \"first_frame\"}
    ],
    \"ratio\": \"16:9\",
    \"duration\": 15
  }" | jq -r '.id')
echo "task_id=$TASK_ID"
```

`duration` 支持 **5 / 10 / 15** 秒（与豆包 Seedance 工具栏一致）。未传默认 **10**；传入其它值会就近映射（例如 `14`→`15`）。

> **注意：** `duration: 15` 通常需要豆包 **专业版加强套餐**。账号未开通时页面会提示「专业版加强套餐专属能力」，任务会快速失败并返回明确错误（也可自动切换到已开通的账号重试）。

> 15 秒实现参考 [LauZzL/doubao-downloader](https://github.com/LauZzL/doubao-downloader)：在提交请求时改写 `chat_ability.ability_param.duration`（`ability_type=17`），并同步点选工具栏时长 chip。
### 3. 轮询任务结果

```bash
curl -s "http://127.0.0.1:8080/api/v3/contents/generations/tasks/$TASK_ID" | jq .
```

任务 `status` 为 `succeeded` 时，`content.video_url` 即为生成的视频地址（已走本地代理）。

**视频模型**（`model` 字段，对应豆包 UI 中的 Seedance 2.0 选项）：

| API `model` 示例 | 豆包 UI |
|------------------|---------|
| `doubao-seedance-2-0-fast`（默认） | Seedance 2.0 Fast |
| `doubao-seedance-2-0-mini` | Seedance 2.0 Mini |
| `fast` / `mini` | 同上（简写） |

未传 `model` 时默认使用 **Fast**。

### 使用火山引擎 Go SDK 调用

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/volcengine/volcengine-go-sdk/service/arkruntime"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
	"github.com/volcengine/volcengine-go-sdk/volcengine"
)

func main() {
	client := arkruntime.NewClientWithApiKey(
		"dummy-key",
		arkruntime.WithBaseUrl("http://127.0.0.1:8080/api/v3"),
	)

	createResp, err := client.CreateContentGenerationTask(context.Background(), model.CreateContentGenerationTaskRequest{
		Model: "doubao-seedance-2-0-fast",
		Content: []*model.CreateContentGenerationContentItem{
			{
				Type: model.ContentGenerationContentItemTypeText,
				Text: volcengine.String("让画面中的角色缓缓向前走动"),
			},
			{
				Type: model.ContentGenerationContentItemTypeImage,
				ImageURL: &model.ImageURL{
					URL: "tos-cn-i-xxx/your-uploaded-uri",
				},
				Role: volcengine.String("first_frame"),
			},
		},
		Ratio:    volcengine.String("16:9"),
		Duration: volcengine.Int64(15),
	})
	if err != nil {
		log.Fatal(err)
	}

	for {
		task, err := client.GetContentGenerationTask(context.Background(), model.GetContentGenerationTaskRequest{
			ID: createResp.ID,
		})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("status:", task.Status)
		if task.Status == model.StatusSucceeded {
			fmt.Println("video:", task.Content.VideoURL)
			return
		}
		if task.Status == model.StatusFailed {
			log.Fatalf("failed: %v", task.Error)
		}
		time.Sleep(3 * time.Second)
	}
}
```

## 实现说明

文生图通过豆包 Samantha 接口 `/samantha/chat/completion` 实现：

- 请求 `content_type=2009`（图片生成 skill）
- 响应解析 `content_type=2010` 中的图片 URL

图生视频通过同一 Samantha 接口实现：

- 请求 `content_type=2020`（视频生成 skill，skill_type=17）
- 先返回 async task id，再轮询 `/samantha/chat/async/stream`
- 响应解析 `content_type=2021` 中的视频 URL

size 与豆包 ratio 映射：

| size | ratio |
|------|-------|
| 1024x1024 | 1:1 |
| 1792x1024 | 16:9 |
| 1024x1792 | 9:16 |
| 1024x768 | 4:3 |
| 768x1024 | 3:4 |

## 后续计划

- b64_json 响应格式
