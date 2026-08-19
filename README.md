# 速传 📨

单人跨设备中转站：一个聊天框形态的时间流，在自己手机 / 电脑 / 平板之间传**文字、密码、文件、截图**，不再为传个东西登录微信。

**一个 Go 二进制搞定一切**：前端（Vue3）构建后直接嵌进二进制，服务器上只需要 `transfer` + 一个数据目录。

## 它长这样

- **时间流单栏**：所有消息按天分组、统一方向排列，手机电脑都好读
- **长文字自动折叠**：超过约 8 行折叠显示「展开全文」
- **图片直接预览**：截图显示缩略图，点击全屏查看、可下载
- **粘贴 / 拖拽上传**：桌面端 Ctrl+V 粘贴截图直接发送，拖文件进窗口即上传；也支持 📎 选择文件
- **历史搜索**：顶部搜索框，全文模糊匹配（含文件名）
- **一键复制**：每条文字消息带复制按钮
- **容量统计**：顶栏实时显示消息数和占用空间
- **清理工具**：按时间批量清理（7 / 30 / 90 天前）、单条删除、一键清空
- **自动备份**：每天凌晨 4 点打包数据库 + 文件到 `data/backups/`，保留最近 7 份，也可在菜单里手动触发

## 安全模型（先读这个）

- 全站**一个访问密码**（`ACCESS_PASSWORD`），登录一次设备记住 10 年
- 必须**走 HTTPS**（nginx 反代 + Let's Encrypt），密码和消息明文存 SQLite——所以服务器本身的安全就是密码库的安全
- 登录接口限速：同一 IP 每分钟最多 10 次尝试
- 服务端口只绑 `127.0.0.1`，公网只能通过 nginx 进入
- ⚠️ 别在公用电脑上用；退出时点菜单里的「退出登录」

## 本地开发

```bash
# 终端 1：后端（API 在 :8787）
go run .

# 终端 2：前端（页面在 :5173，/api 自动代理到 8787）
cd web && npm install && npm run dev
```

构建单二进制：

```bash
make build   # 产出 ./transfer
./transfer   # 未设 ACCESS_PASSWORD 时会打印一个随机密码
```

## 部署到服务器（Docker + nginx）

前置条件：一台国内云服务器（已有 Docker）、一个已备案域名。

**1. 上传代码到服务器**

```bash
scp -r transfer/ user@your-server:/opt/transfer
# 或 git clone 你自己的仓库
```

**2. 配置密码**

```bash
cd /opt/transfer
cp .env.example .env
vi .env        # 设置 ACCESS_PASSWORD（强密码！）
```

**3. 启动容器**

```bash
docker compose up -d --build
# 验证：curl 127.0.0.1:8787 应返回页面
```

**4. 配 nginx + HTTPS**

把 `nginx.conf.example` 里的域名换成你的（比如 `chuan.你的域名.com`），放到 `/etc/nginx/conf.d/transfer.conf`，然后：

```bash
sudo nginx -t && sudo systemctl reload nginx
sudo certbot --nginx -d chuan.你的域名.com   # 自动签发证书并改写配置
```

**5. 手机上加到主屏**

浏览器打开 `https://chuan.你的域名.com`，输入密码，完成。Safari 可「添加到主屏幕」，Chrome 可「添加到主屏幕/安装应用」，之后点图标即用。

### 升级 / 迁移

```bash
cd /opt/transfer
git pull            # 或重新 scp
docker compose up -d --build
```

所有数据（数据库、文件、备份）都在 `./data/` 目录，换机器时整个目录拷走即可。

## 数据与备份

```
data/
├── transfer.db        # SQLite（消息、token）
├── transfer.db-wal    # WAL 日志（正常现象）
├── uploads/           # 上传的文件
└── backups/           # 每日自动备份（tar.gz，保留 7 份）
```

备份内容 = 数据库快照（`VACUUM INTO`，运行中一致）+ 全部上传文件。恢复：解压 tar.gz，把 `backup.db` 改名为 `transfer.db`、`uploads/` 覆盖回去，重启容器。

## 常见问题

**Q：文件能传多大？** 单文件上限 50MB（截图、文档场景）。更大的东西建议走网盘。

**Q：换新设备怎么办？** 打开网址输一次密码就行。旧设备想踢掉：任一设备「退出登录」只注销当前设备；想全部注销需清空 `data/transfer.db` 里的 `tokens` 表（或直接删库重启——消息会没，慎重）。

**Q：消息能撤回吗？** 不能撤回，但可以单条删除、批量清理、一键清空。

**Q：多久同步一次？** 页面开着时每 5 秒轮询一次新消息，切回标签页立即刷新。

**Q：忘了密码？** 改 `.env` 里的 `ACCESS_PASSWORD` 然后 `docker compose up -d` 重启。旧登录 token 仍有效，直到对方退出登录。

## 技术栈

Go 1.23（标准库路由 + modernc.org/sqlite 纯 Go 驱动，无 CGO）· Vue 3 + Vite · embed 单二进制交付 · Docker 三阶段构建

## License

MIT
