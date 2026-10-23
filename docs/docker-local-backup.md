# MiniProbe Docker 每日本地备份

适用：已迁移到 `/opt/miniprobe-docker` 的 MiniProbe Docker Server，Debian/Ubuntu + systemd。仅在 **Docker 宿主机** 安装，和 HomeSphere 没有任何共享目录或服务；不修改 Agent、Server 镜像或 Cloudflare DNS。

## 功能

- 每天日本时间 **04:30–04:35** 自动备份一次；启动时如果错过当天的定时任务，systemd `Persistent=true` 会补一次。
- 首次安装先试运行一份，成功才启用定时器。
- `docker pause` 仅短暂冻结 MiniProbe **Server** 以复制一致性快照，复制最长 **20 秒**；**不会停止或重建 Server 与 cloudflared 容器**。随后立即解除暂停，在后台进行压缩和校验。短暂暂停期间 HTTP 上报可能排队/短时失败，正常不会达到 2 分钟离线告警阈值；实际效果以实机为准。
- `EXIT` 清理陷阱和 systemd `ExecStopPost` 双重保证尽量解除暂停；如果 Docker 本身无法响应，必须人工核对容器，不能声称一定恢复。
- 快照包括：`data/`（节点、主身份、流量历史、网络历史、订阅数据）、独立 Tunnel Token、`.env` 和两份 Compose 配置及 `manage.sh`。**不备份** HomeSphere / PostgreSQL / Media Bridge。
- 备份存放 `/var/backups/miniprobe-docker/`，目录 `0700`、压缩包和校验文件 `0600`，仅 root 可读，不上传到云端，也不增加额外密钥。
- 备份每次验证数据库格式、gzip、tar 目录、SHA256。只在成功后清理：保留最近 **7 份**、最多 **1 GiB** 压缩包；如果接近总量上限会提前淘汰旧备份，但至少保留最近一份。剩余空间不足时失败并保留旧备份。

**安全提醒：备份包含完整 Server 签名身份、节点凭据、订阅信息和 Cloudflare Tunnel Token，请勿发送到聊天、GitHub 或公开网盘。** 此方案抵御误操作/数据损坏，不抵御 VPS 整机丢失或 root 权限被盗；若未来需要异地备份，另行设计。

## 安装

先确保 Docker Server 显示 `healthy`，节点已恢复；备份脚本只适用于已经完成迁移的 Docker 宿主机。

将仓库中的 `deploy/docker/backup.sh`、`deploy/docker/backup-install.sh` 复制到宿主机 `/opt/miniprobe-docker/`，以 root 运行：

```bash
bash /opt/miniprobe-docker/backup-install.sh
```

安装器注册以下 systemd 单元，并在第一次备份成功后启用 timer：

- `miniprobe-docker-backup.service`
- `miniprobe-docker-backup.timer`

每天按 **Asia/Tokyo** 计时，和主机系统时区无关；故意错开 HomeSphere 的 04:00 备份。

## 检查 / 手动执行

```bash
# 查看下次计划运行日期
systemctl list-timers --all miniprobe-docker-backup.timer

# 手工立即创建一份；不会重复启用计时器
systemctl start miniprobe-docker-backup.service

# 检查执行是否成功，不打印压缩包内容
journalctl -u miniprobe-docker-backup.service -n 20 --no-pager

# 列出本地备份文件名和大小
ls -lh /var/backups/miniprobe-docker/miniprobe-*.tar.gz
```

`sha256sum -c xxx.tar.gz.sha256` 应在备份目录内执行，校验文件已自带相对文件名。

取消自动备份：

```bash
systemctl disable --now miniprobe-docker-backup.timer
```

该操作不会删除旧备份或停掉 MiniProbe Server/Tunnel。

## 恢复原则

**恢复是独立的灾难恢复操作，不能覆盖当前正在写入的数据。**

1. 以 root 检查 `sha256sum -c` 和 `tar -tzf`，在 root-only 的临时目录解压，核对 `data/miniprobe.json`、历史目录与 `secrets/cloudflared.token` 存在且有效。
2. 停止自动备份 timer、独立 MiniProbe cloudflared 和 Server；先另外保存当前 `data/` 与 `secrets/`，不可直接丢弃。
3. 仅将备份中的 MiniProbe `data/`、`secrets/` 与配置恢复到 `/opt/miniprobe-docker/`。目录属主和权限：`data` 及下层归 `10001:10001`，`data/` 为 `0700`；`secrets/cloudflared.token` 属主 `10001:10001`、模式 `0600`，`secrets/` 宿主机模式 `0700`。同时对照原部署配置，谨慎处理后续变更，避免回退较新镜像/配置或丢失备份后的数据。
4. `sh /opt/miniprobe-docker/manage.sh start` 恢复 Server，健康检查通过后才执行 `sh manage.sh tunnel-start`，核实节点、历史数据与 HomeSphere，最后恢复备份 timer。
5. 始终不要同时在新旧 VPS 上启动接收同一组 Agent 数据的两个 Server。

切勿使用 `--init-only` 或从空目录重新生成密钥来“修复”备份故障。
