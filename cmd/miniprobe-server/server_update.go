package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const serverReleaseAPI = "https://api.github.com/repos/Jackyhuang83/MiniProbe/releases?per_page=5"
const serverReleaseBase = "https://github.com/Jackyhuang83/MiniProbe/releases/download/"

var serverUpdateVersionPattern = regexp.MustCompile("^v([0-9]+)\\.([0-9]+)\\.([0-9]+)(?:-(alpha|beta|rc)(?:[.-]([0-9]+))?)?$")

func serverVersionTuple(version string) ([]int, bool) {
	match := serverUpdateVersionPattern.FindStringSubmatch(version)
	if len(match) == 0 {
		return nil, false
	}
	out := make([]int, 5)
	for i := 1; i <= 3; i++ {
		n, err := strconv.Atoi(match[i])
		if err != nil || n < 0 {
			return nil, false
		}
		out[i-1] = n
	}
	switch match[4] {
	case "alpha":
		out[3] = 0
	case "beta":
		out[3] = 1
	case "rc":
		out[3] = 2
	case "":
		out[3] = 3
	default:
		return nil, false
	}
	if match[5] != "" {
		n, err := strconv.Atoi(match[5])
		if err != nil || n < 0 {
			return nil, false
		}
		out[4] = n
	}
	return out, true
}

func compareServerVersions(a, b string) (int, error) {
	x, okX := serverVersionTuple(a)
	y, okY := serverVersionTuple(b)
	if !okX || !okY {
		return 0, fmt.Errorf("版本号格式不支持: %q / %q", a, b)
	}
	for i := range x {
		if x[i] > y[i] {
			return 1, nil
		}
		if x[i] < y[i] {
			return -1, nil
		}
	}
	return 0, nil
}

func serverUpdateClient() *http.Client {
	return &http.Client{
		Timeout: 90 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" || len(via) >= 6 {
				return errors.New("更新下载要求 HTTPS 且最多 5 次重定向")
			}
			return nil
		},
	}
}

func fetchSmallUpdateText(client *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MiniProbe-Server-Updater")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("更新元数据超过大小限制")
	}
	return b, nil
}

func latestMiniProbeServerRelease(client *http.Client) (string, error) {
	return latestMiniProbeServerReleaseAt(client, serverReleaseAPI)
}

// Fetch a bounded page of recent releases. Each GitHub release includes
// verbose assets and metadata, making even 20 entries exceed 128 KiB.
// Keep a hard 1 MiB cap, but only request the most recent five releases.
func latestMiniProbeServerReleaseAt(client *http.Client, releaseAPI string) (string, error) {
	data, err := fetchSmallUpdateText(client, releaseAPI, 1<<20)
	if err != nil {
		return "", err
	}
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		return "", err
	}
	latest := ""
	for _, item := range items {
		tag, _ := item["tag_name"].(string)
		if item["draft"] == true || item["published_at"] == nil {
			continue
		}
		if _, ok := serverVersionTuple(tag); !ok {
			continue
		}
		if latest == "" {
			latest = tag
			continue
		}
		if cmp, err := compareServerVersions(tag, latest); err == nil && cmp > 0 {
			latest = tag
		}
	}
	if latest == "" {
		return "", errors.New("未从官方 Releases 找到有效发布版本")
	}
	return latest, nil
}

// Use checksum filenames as an allowlist. Never accept filenames from a
// downloaded manifest as filesystem paths or execute remote installer scripts.
func serverReleaseChecksums(manifest string, assets []string) (map[string]string, error) {
	allow := make(map[string]bool, len(assets))
	for _, asset := range assets {
		allow[asset] = true
	}
	out := make(map[string]string, len(assets))
	for _, line := range strings.Split(manifest, "\n") {
		parts := strings.Fields(line)
		if len(parts) != 2 || !allow[strings.TrimPrefix(parts[1], "*")] {
			continue
		}
		name := strings.TrimPrefix(parts[1], "*")
		if len(parts[0]) != 64 {
			return nil, fmt.Errorf("校验长度错误: %s", name)
		}
		if _, err := hex.DecodeString(parts[0]); err != nil {
			return nil, fmt.Errorf("无效 SHA256: %s", name)
		}
		if _, duplicate := out[name]; duplicate {
			return nil, fmt.Errorf("重复的 SHA256 记录: %s", name)
		}
		out[name] = strings.ToLower(parts[0])
	}
	for _, asset := range assets {
		if out[asset] == "" {
			return nil, fmt.Errorf("SHA256SUMS 缺少 %s", asset)
		}
	}
	return out, nil
}

func fetchVerifiedReleaseBinary(client *http.Client, url, dest, expectedSHA string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "MiniProbe-Server-Updater")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败 HTTP %d", resp.StatusCode)
	}
	file, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	// All supported MiniProbe binaries are < 20 MiB. Avoid unbounded downloads.
	n, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(resp.Body, (64<<20)+1))
	if err != nil {
		return err
	}
	if n <= 0 || n > 64<<20 {
		return errors.New("下载文件大小异常")
	}
	if got := hex.EncodeToString(hash.Sum(nil)); !strings.EqualFold(got, expectedSHA) {
		return errors.New("SHA256 校验不通过，不安装该版本")
	}
	if err := file.Sync(); err != nil {
		return err
	}
	return file.Chmod(0755)
}

func copyServerUpdateBackup(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if err != nil {
		return err
	}
	_, writeErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if writeErr != nil {
		return writeErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Chmod(destination, 0755)
}

func runningServerUpdateVersion(socket string, timeout time.Duration, want string) error {
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		var out struct {
			Version string
		}
		// The local, root-only Unix socket avoids dependence on Cloudflare,
		// DNS, public IPs, dashboard cookies or listen mode.
		var body map[string]any
		c := newLocalClient(socket)
		if err := c.request(http.MethodGet, "/v1/server-version", nil, &body); err == nil {
			out.Version, _ = body["server_version"].(string)
			if out.Version == want {
				return nil
			}
			last = fmt.Errorf("运行版本=%s，目标=%s", out.Version, want)
		} else {
			last = err
		}
		time.Sleep(500 * time.Millisecond)
	}
	if last == nil {
		last = errors.New("版本检查超时")
	}
	return last
}

func updateMiniProbeServer(client *http.Client, tag, socket string) error {
	if os.Geteuid() != 0 {
		return errors.New("更新 Server 需要 root SSH 权限")
	}
	arch := runtime.GOARCH
	if arch != "amd64" && arch != "arm64" {
		return fmt.Errorf("不支持的 Server 架构: %s", arch)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return err
	}
	if filepath.Base(exe) != "miniprobe-server" {
		return errors.New("无法确认当前 Server 程序路径，拒绝替换")
	}
	if info, err := os.Lstat(exe); err != nil || !info.Mode().IsRegular() {
		return errors.New("Server 不是普通文件，拒绝替换")
	}
	dir := filepath.Dir(exe)
	downloadDir := filepath.Join(dir, "downloads")
	assets := []string{
		"miniprobe-server-linux-" + arch,
		"miniprobe-agent-linux-amd64",
		"miniprobe-agent-linux-arm64",
		"miniprobe-agent-linux-armv7",
	}
	base := serverReleaseBase + tag + "/"
	manifest, err := fetchSmallUpdateText(client, base+"SHA256SUMS", 32<<10)
	if err != nil {
		return fmt.Errorf("获取校验文件失败: %w", err)
	}
	sums, err := serverReleaseChecksums(string(manifest), assets)
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(dir, ".server-update-")
	if err != nil {
		return err
	}
	// Keep a recovery copy if anything after replacement fails.
	cleanStage := true
	defer func() {
		if cleanStage {
			_ = os.RemoveAll(stage)
		}
	}()
	for _, asset := range assets {
		fmt.Println("下载并校验:", asset)
		if err := fetchVerifiedReleaseBinary(client, base+asset, filepath.Join(stage, asset), sums[asset]); err != nil {
			return fmt.Errorf("%s: %w", asset, err)
		}
	}
	type replacement struct {
		source, target, backup string
	}
	changes := []replacement{
		{filepath.Join(stage, assets[0]), exe, filepath.Join(stage, "backup-server")},
	}
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		return err
	}
	for _, asset := range assets[1:] {
		changes = append(changes, replacement{
			filepath.Join(stage, asset), filepath.Join(downloadDir, asset), filepath.Join(stage, "backup-"+asset),
		})
	}
	for _, item := range changes {
		if err := copyServerUpdateBackup(item.target, item.backup); err != nil {
			return fmt.Errorf("备份旧程序失败（更新尚未进行）: %w", err)
		}
	}
	rollback := func(cause error) error {
		for _, item := range changes {
			// Backups reside on the same filesystem, so rollback is atomic.
			if err := os.Rename(item.backup, item.target); err != nil {
				cleanStage = false
				return fmt.Errorf("%w；自动回滚失败: %v；备份保留在 %s", cause, err, stage)
			}
		}
		if err := restartMiniProbeServer(); err != nil {
			cleanStage = false
			return fmt.Errorf("%w；已恢复旧程序但重启失败: %v；备份目录 %s", cause, err, stage)
		}
		return fmt.Errorf("%w；已回滚旧版 Server / Agent 文件", cause)
	}
	for _, item := range changes {
		if err := os.Rename(item.source, item.target); err != nil {
			return rollback(fmt.Errorf("更新文件失败: %w", err))
		}
	}
	if err := restartMiniProbeServer(); err != nil {
		return rollback(fmt.Errorf("新版本重启失败: %w", err))
	}
	if err := runningServerUpdateVersion(socket, 15*time.Second, strings.TrimPrefix(tag, "v")); err != nil {
		return rollback(fmt.Errorf("新版本运行校验失败: %w", err))
	}
	return nil
}

func manageServerUpdate(reader *bufio.Reader, socket string) bool {
	if isDockerDeployment() {
		fmt.Println("Docker 模式禁止在容器内替换二进制或重启宿主机服务。")
		fmt.Println("请通过 MiniProbe Docker 管理脚本升级镜像并做健康检查；失败时可回滚旧镜像。")
		return false
	}
	fmt.Println("\n========== Server 更新 ==========")
	fmt.Println("当前 Server:", serverVersion)
	fmt.Println("目标 Agent:", agentTargetVersion)
	fmt.Println("从 MiniProbe 官方 GitHub Releases 查询最新版本（不通过第三方镜像）。")
	client := serverUpdateClient()
	latest, err := latestMiniProbeServerRelease(client)
	if err != nil {
		fmt.Println("检查更新失败:", err)
		return false
	}
	fmt.Println("GitHub 最新发布:", latest)
	cmp, err := compareServerVersions(latest, "v"+serverVersion)
	if err != nil {
		fmt.Println("版本比较失败:", err)
		return false
	}
	if cmp <= 0 {
		fmt.Println("Server 已是最新版本；不会降级或重复安装。")
		return false
	}
	fmt.Println("更新前将备份 Server 和 Agent 安装文件，下载 SHA256 验证并重启 Server。")
	fmt.Println("现有数据库、Token、流量统计、Dashboard 密码、Cloudflare Tunnel 配置不会修改。")
	if strings.ToUpper(prompt(reader, "输入 YES 确认升级 Server（其他输入取消）: ")) != "YES" {
		fmt.Println("已取消。")
		return false
	}
	if err := updateMiniProbeServer(client, latest, socket); err != nil {
		fmt.Println("Server 更新失败:", err)
		return false
	}
	fmt.Printf("Server 更新成功：%s；当前管理进程为旧版本，请重新运行 miniprobe。\n", latest)
	return true
}
