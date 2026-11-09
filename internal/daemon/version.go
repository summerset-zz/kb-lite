package daemon

// Version 是当前版本号。`kb-lite version`、启动横幅、/api/status 都用它。
//
// 打 tag 之前一律带 `-dev`：这样别人报 issue 时，一眼就能看出跑的是未发布的中间态。
const Version = "0.3.0-dev"
