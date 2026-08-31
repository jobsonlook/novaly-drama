# 豆包无水印下载（Chrome 插件）

在豆包对话页右下角显示 **「无水印下载」** 按钮，一点即可下载去掉「豆包AI生成」的视频。

## 安装

1. 打开 Chrome → `chrome://extensions`
2. 右上角打开 **开发者模式**
3. **加载已解压的扩展程序**
4. 选本目录：`doubao-web-api/chrome-extension`
5. 打开任意豆包视频对话页，右下角会出现绿色按钮

## 用法

1. 打开带视频的对话（或刷新），等视频出现
2. 按钮若显示 `无水印下载 (1)` 说明已抓到 `fallback_api`
3. 点击按钮 → 浏览器开始下载 `doubao-clean-*.mp4`
4. 到 Novaly 对应镜头点 **替换视频** 上传

**不要用**豆包自带蓝色「保存」——那个是有水印版。

## 原理

与 `doubao-web-api` 相同：

1. 拦截 `/im/chain/single` 等响应，抓取 `fallback_api`
2. 请求时设置 `logo_type=unwatermarked`（及 `channel=no`、`codec_type=8`）
3. 解密 `main_url` 得到干净播放地址并下载

## 注意

- 若提示未捕获到 `fallback_api`：刷新页面，点开视频预览后再试
- 链接有时效，生成后尽快下
- 仅用于你自己账号生成内容的备份/去水印工作流
