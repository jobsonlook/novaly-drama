#!/usr/bin/env bash
# 关羽场景视频测试：图1场景 + 图2关羽 + 台词（普通视频生成模式，无音频）
#
# 用法:
#   ./scripts/test-guanyu-video.sh /path/to/zz
#
# 前置:
#   1. ./scripts/start-chrome.sh  并已登录 doubao.com
#   2. go run ./cmd/server   （默认 VIDEO_UI_MODE=skill）

set -euo pipefail

API="${API:-http://127.0.0.1:8080}"
ASSET_DIR="${1:-}"

if [[ -z "$ASSET_DIR" || ! -d "$ASSET_DIR" ]]; then
  echo "用法: $0 /path/to/zz" >&2
  echo "目录需包含: 场景1的背视图.jpg  关羽全彩.png" >&2
  exit 1
fi

SCENE="$ASSET_DIR/场景1的背视图.jpg"
CHAR="$ASSET_DIR/关羽全彩.png"

for f in "$SCENE" "$CHAR"; do
  if [[ ! -f "$f" ]]; then
    echo "缺少文件: $f" >&2
    exit 1
  fi
done

upload() {
  local file="$1"
  curl -sS -X POST "$API/api/v3/files/uploads" -F "file=@${file}" | jq -er '.uri'
}

echo ">> 上传参考素材..."
URI_SCENE=$(upload "$SCENE")
URI_CHAR=$(upload "$CHAR")
echo "  图1(场景): $URI_SCENE"
echo "  图2(关羽): $URI_CHAR"

PROMPT='图1是场景，图2是关羽。

关羽在场景里面说话：某一生纵横天下，温酒斩华雄、千里走单骑、过五关斩六将！区区江东鼠辈，布下雕虫小技，也敢困我关云长？'

BODY=$(jq -n \
  --arg prompt "$PROMPT" \
  --arg scene "$URI_SCENE" \
  --arg char "$URI_CHAR" \
  '{
    model: "doubao-seedance-2-0-fast",
    content: [
      {type: "text", text: $prompt},
      {type: "image_url", image_url: {url: $scene}, role: "reference"},
      {type: "image_url", image_url: {url: $char}, role: "reference"}
    ],
    ratio: "16:9",
    duration: 15
  }')

echo ">> 创建视频任务..."
TASK=$(curl -sS -X POST "$API/api/v3/contents/generations/tasks" \
  -H "Content-Type: application/json" \
  -d "$BODY" | jq -er '.id')
echo "  task_id=$TASK"

echo ">> 轮询结果 (Ctrl+C 停止)..."
while true; do
  RESP=$(curl -sS "$API/api/v3/contents/generations/tasks/$TASK")
  STATUS=$(echo "$RESP" | jq -er '.status' 2>/dev/null || echo "unknown")
  echo "  status=$STATUS"
  if [[ "$STATUS" == "succeeded" ]]; then
    echo "$RESP" | jq . 2>/dev/null || echo "$RESP"
    echo "video_url: $(echo "$RESP" | jq -r '.content.video_url // empty' 2>/dev/null)"
    exit 0
  fi
  if [[ "$STATUS" == "failed" ]]; then
    echo "$RESP" | jq . 2>/dev/null || echo "$RESP"
    exit 1
  fi
  sleep 5
done
