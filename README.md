# Novaly Drama 单机版

基于 Novaly + 内置 doubao-web-api。SQLite、上传图片、生成视频保存在本机，不使用 COS/TOS 对象存储；AI 生成仍需联网。

## 启动

需要 Go、Node.js/npm、Google Chrome；视频处理需要 ffmpeg。

```bash
./start.sh
```

首次运行自动安装前端依赖并构建两个 Go 服务，之后直接启动。打开 http://127.0.0.1:8085，在「设置中心」点击「启动 doubao-web-api」。首次启动会打开独立 Chrome，手动登录豆包；「豆包账号管理」可管理账号。启动中可等待状态刷新，失败查看日志。

- Novaly：127.0.0.1:8085，仅本机监听。
- 豆包 API / 账号管理：127.0.0.1:8086，仅本机监听。
- 专用 Chrome 调试端口：9322 起，与原服务的 9222 分开。
- Novaly 数据：backend/data/，数据库为 backend/data/novaly.db。
- 豆包账号与日志：doubao-web-api/data/；日志 service.log。
- 豆包登录会话：doubao-web-api/session/。包含登录信息，请勿提交或分享。

默认图片模型为 Seedream Web，视频模型为 Seedance Web。文本写作、语音及其他云模型需要在设置中心配置对应凭据；未复制原项目密钥、数据库或登录会话。

Ctrl+C 退出 Novaly 时，会通知它启动的豆包子服务退出。停止豆包会中断进行中的生成，请先等待完成。不会接管或停止端口上已经存在的外部服务。修改源码后运行 `./scripts/build.sh` 重新构建。

前端开发可在 frontend 执行 `npm run dev`，代理到 8085 后端。此版本服务管理固定使用 8086，请勿单独修改豆包供应商地址。备份请先停止服务，再复制 backend/data 与 doubao-web-api/data、session。
