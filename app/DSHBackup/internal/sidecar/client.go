package sidecar

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"DSHBackup/internal/logging"
)

type RPCError struct {
	Code    int64           `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	if len(e.Data) > 0 {
		return fmt.Sprintf("%s（%s）", e.Message, strings.TrimSpace(string(e.Data)))
	}
	return e.Message
}

var ErrCancelled = &RPCError{Code: -32800, Message: "任务已取消"}

func IsCancelled(err error) bool {
	var rpc *RPCError
	if errors.As(err, &rpc) {
		return rpc.Code == -32800
	}
	return errors.Is(err, context.Canceled)
}

type Client struct {
	path   string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	logger *logging.Logger

	writeMu   sync.Mutex
	pendingMu sync.Mutex
	pending   map[int64]chan callResult
	eventsMu  sync.Mutex
	events    map[string]func(Progress)
	closed    bool
	nextID    atomic.Int64

	stderrMu   sync.Mutex
	stderrTail []string
	version    string
}

type callResult struct {
	result json.RawMessage
	err    error
}

type envelope struct {
	ID     *int64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *RPCError       `json:"error"`
}

type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

func Start(path string, logger *logging.Logger) (*Client, error) {
	if path == "" {
		return nil, errors.New("找不到 Rust sidecar（dsh-backup-core.exe）")
	}
	cmd := exec.Command(path)
	configureProcess(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	client := &Client{
		path:    path,
		cmd:     cmd,
		stdin:   stdin,
		logger:  logger,
		pending: make(map[int64]chan callResult),
		events:  make(map[string]func(Progress)),
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("无法启动 Rust sidecar：%w", err)
	}
	go client.readStdout(stdout)
	go client.readStderr(stderr)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var pong struct {
		Version  string `json:"version"`
		Protocol int    `json:"protocol"`
	}
	if err := client.Call(ctx, "ping", map[string]any{}, &pong); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("sidecar 未响应：%w", err)
	}
	client.version = pong.Version
	return client, nil
}

func (c *Client) Version() string {
	return c.version
}

func (c *Client) readStdout(reader io.Reader) {
	buffered := bufio.NewReaderSize(reader, 64*1024)
	for {
		line, err := buffered.ReadBytes('\n')
		if len(line) > 0 {
			c.handleLine(line)
		}
		if err != nil {
			break
		}
	}
	c.failAll(fmt.Errorf("sidecar 进程已退出%s", c.stderrSuffix()))
}

func (c *Client) handleLine(raw []byte) {
	var message envelope
	if err := json.Unmarshal(raw, &message); err != nil {
		c.logf("sidecar.invalid-line", "无法解析 sidecar 输出：%v", err)
		return
	}
	if message.Method != "" {
		if message.Method == "progress" {
			var progress Progress
			if err := json.Unmarshal(message.Params, &progress); err != nil {
				return
			}
			c.eventsMu.Lock()
			handler := c.events[progress.TaskID]
			c.eventsMu.Unlock()
			if handler != nil {
				handler(progress)
			}
		}
		return
	}
	if message.ID == nil {
		return
	}
	c.pendingMu.Lock()
	channel := c.pending[*message.ID]
	delete(c.pending, *message.ID)
	c.pendingMu.Unlock()
	if channel == nil {
		return
	}
	if message.Error != nil {
		channel <- callResult{err: message.Error}
		return
	}
	channel <- callResult{result: message.Result}
}

func (c *Client) readStderr(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 16*1024), 256*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		c.stderrMu.Lock()
		c.stderrTail = append(c.stderrTail, line)
		if len(c.stderrTail) > 40 {
			c.stderrTail = c.stderrTail[len(c.stderrTail)-40:]
		}
		c.stderrMu.Unlock()
		c.logf("sidecar.stderr", "%s", line)
	}
}

func (c *Client) stderrSuffix() string {
	c.stderrMu.Lock()
	defer c.stderrMu.Unlock()
	if len(c.stderrTail) == 0 {
		return ""
	}
	start := len(c.stderrTail) - 3
	if start < 0 {
		start = 0
	}
	return "：" + strings.Join(c.stderrTail[start:], "；")
}

func (c *Client) logf(event, format string, args ...any) {
	if c.logger == nil {
		return
	}
	c.logger.Info(event, logging.Redact(fmt.Sprintf(format, args...)), nil)
}

func (c *Client) failAll(err error) {
	c.pendingMu.Lock()
	pending := c.pending
	c.pending = make(map[int64]chan callResult)
	c.pendingMu.Unlock()
	for _, channel := range pending {
		select {
		case channel <- callResult{err: err}:
		default:
		}
	}
}

func (c *Client) Call(ctx context.Context, method string, params any, result any) error {
	return c.call(ctx, method, params, result)
}

func (c *Client) CallTask(ctx context.Context, taskID, method string, params map[string]any, result any, onEvent func(Progress)) error {
	if taskID == "" {
		return errors.New("任务 ID 不能为空")
	}
	if params == nil {
		params = map[string]any{}
	}
	params["taskId"] = taskID
	if onEvent != nil {
		c.eventsMu.Lock()
		c.events[taskID] = onEvent
		c.eventsMu.Unlock()
		defer func() {
			c.eventsMu.Lock()
			delete(c.events, taskID)
			c.eventsMu.Unlock()
		}()
	}
	return c.call(ctx, method, params, result)
}

func (c *Client) call(ctx context.Context, method string, params any, result any) error {
	if params == nil {
		params = map[string]any{}
	}
	id := c.nextID.Add(1)
	channel := make(chan callResult, 1)
	c.pendingMu.Lock()
	if c.closed {
		c.pendingMu.Unlock()
		return errors.New("sidecar 已关闭")
	}
	c.pending[id] = channel
	c.pendingMu.Unlock()

	raw, err := json.Marshal(request{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		c.removePending(id)
		return err
	}
	c.writeMu.Lock()
	_, writeErr := c.stdin.Write(append(raw, '\n'))
	c.writeMu.Unlock()
	if writeErr != nil {
		c.removePending(id)
		return fmt.Errorf("写入 sidecar 失败：%w", writeErr)
	}

	select {
	case <-ctx.Done():
		c.removePending(id)
		return ctx.Err()
	case response := <-channel:
		if response.err != nil {
			return response.err
		}
		if result != nil && len(response.result) > 0 {
			if err := json.Unmarshal(response.result, result); err != nil {
				return fmt.Errorf("解析 sidecar 响应失败：%w", err)
			}
		}
		return nil
	}
}

func (c *Client) removePending(id int64) {
	c.pendingMu.Lock()
	delete(c.pending, id)
	c.pendingMu.Unlock()
}

func (c *Client) Cancel(taskID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var result struct {
		Cancelled bool `json:"cancelled"`
	}
	return c.Call(ctx, "cancel", map[string]any{"taskId": taskID}, &result)
}

func (c *Client) Close() error {
	c.pendingMu.Lock()
	if c.closed {
		c.pendingMu.Unlock()
		return nil
	}
	c.closed = true
	c.pendingMu.Unlock()
	_ = c.stdin.Close()
	done := make(chan struct{})
	go func() {
		_ = c.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = c.cmd.Process.Kill()
		<-done
	}
	c.failAll(errors.New("sidecar 已关闭"))
	return nil
}

type CheckResult struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	Version string `json:"version"`
}

func (c *Client) Check7z(ctx context.Context, path string) (CheckResult, error) {
	var result CheckResult
	err := c.Call(ctx, "check_7z", map[string]any{"path": path}, &result)
	return result, err
}

type HashResult struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func (c *Client) HashFile(ctx context.Context, path string) (HashResult, error) {
	var result HashResult
	err := c.Call(ctx, "hash_file", map[string]any{"path": path}, &result)
	return result, err
}

type KeyInfo struct {
	Exists      bool   `json:"exists"`
	Generated   bool   `json:"generated"`
	PublicKey   string `json:"publicKey"`
	KeyID       string `json:"keyId"`
	PrivatePath string `json:"privatePath"`
	Error       string `json:"error,omitempty"`
}

func (c *Client) KeysEnsure(ctx context.Context, dir string) (KeyInfo, error) {
	var result KeyInfo
	err := c.Call(ctx, "keys_ensure", map[string]any{"dir": dir}, &result)
	return result, err
}

func (c *Client) KeysInfo(ctx context.Context, dir string) (KeyInfo, error) {
	var result KeyInfo
	err := c.Call(ctx, "keys_info", map[string]any{"dir": dir}, &result)
	return result, err
}

func (c *Client) WrapPassword(ctx context.Context, dir, password string) (string, string, error) {
	var result struct {
		Wrapped string `json:"wrapped"`
		KeyID   string `json:"keyId"`
	}
	err := c.Call(ctx, "wrap_password", map[string]any{"dir": dir, "password": password}, &result)
	return result.Wrapped, result.KeyID, err
}

func (c *Client) UnwrapPassword(ctx context.Context, dir, wrapped string) (string, error) {
	var result struct {
		Password string `json:"password"`
	}
	err := c.Call(ctx, "unwrap_password", map[string]any{"dir": dir, "wrapped": wrapped}, &result)
	return result.Password, err
}

type Progress struct {
	TaskID     string  `json:"taskId"`
	Phase      string  `json:"phase"`
	Percent    float64 `json:"percent"`
	Message    string  `json:"message"`
	BytesDone  int64   `json:"bytesDone"`
	BytesTotal int64   `json:"bytesTotal"`
}

type StageRoot struct {
	Name       string   `json:"name"`
	Source     string   `json:"source"`
	ExcludeTop []string `json:"excludeTop"`
	ExcludeRel []string `json:"excludeRel"`
}

type StageResult struct {
	Files      int64    `json:"files"`
	Bytes      int64    `json:"bytes"`
	TotalBytes int64    `json:"totalBytes"`
	Hardlinked int64    `json:"hardlinked"`
	Skipped    int64    `json:"skipped"`
	Warnings   []string `json:"warnings"`
	StagingDir string   `json:"stagingDir"`
}

func (c *Client) Stage(ctx context.Context, taskID, stagingDir string, roots []StageRoot, onEvent func(Progress)) (StageResult, error) {
	var result StageResult
	params := map[string]any{"stagingDir": stagingDir, "roots": roots}
	err := c.CallTask(ctx, taskID, "stage", params, &result, onEvent)
	return result, err
}

type Compression struct {
	Level        int      `json:"level"`
	DictionaryMB int      `json:"dictionaryMb,omitempty"`
	Threads      int      `json:"threads,omitempty"`
	ExtraArgs    []string `json:"extraArgs"`
}

type BackupRequest struct {
	SevenZip    string
	StagingDir  string
	Output      string
	Password    string
	Compression Compression
	TotalBytes  int64
}

type BackupResult struct {
	Size     int64    `json:"size"`
	SHA256   string   `json:"sha256"`
	Warnings []string `json:"warnings"`
	ExitCode int      `json:"exitCode"`
}

func (c *Client) Backup(ctx context.Context, taskID string, req BackupRequest, onEvent func(Progress)) (BackupResult, error) {
	var result BackupResult
	params := map[string]any{
		"sevenZip":    req.SevenZip,
		"stagingDir":  req.StagingDir,
		"output":      req.Output,
		"compression": req.Compression,
		"totalBytes":  req.TotalBytes,
	}
	if req.Password != "" {
		params["password"] = req.Password
	}
	err := c.CallTask(ctx, taskID, "backup", params, &result, onEvent)
	return result, err
}

type ListResult struct {
	Entries []string `json:"entries"`
	Files   int64    `json:"files"`
	Bytes   int64    `json:"bytes"`
}

func (c *Client) ListArchive(ctx context.Context, sevenZip, archive, password string) (ListResult, error) {
	params := map[string]any{"sevenZip": sevenZip, "archive": archive}
	if password != "" {
		params["password"] = password
	}
	var result ListResult
	err := c.Call(ctx, "list_archive", params, &result)
	return result, err
}

func (c *Client) ValidateArchive(ctx context.Context, sevenZip, archive, password string) (ListResult, error) {
	params := map[string]any{"sevenZip": sevenZip, "archive": archive}
	if password != "" {
		params["password"] = password
	}
	var result ListResult
	err := c.Call(ctx, "validate_archive", params, &result)
	return result, err
}

type TestResult struct {
	OK       bool     `json:"ok"`
	ExitCode int      `json:"exitCode"`
	Warnings []string `json:"warnings"`
}

func (c *Client) TestArchive(ctx context.Context, taskID, sevenZip, archive, password string, onEvent func(Progress)) (TestResult, error) {
	var result TestResult
	params := map[string]any{"sevenZip": sevenZip, "archive": archive}
	if password != "" {
		params["password"] = password
	}
	err := c.CallTask(ctx, taskID, "test_archive", params, &result, onEvent)
	return result, err
}

type ExtractResult struct {
	Files    int64    `json:"files"`
	Bytes    int64    `json:"bytes"`
	Warnings []string `json:"warnings"`
	ExitCode int      `json:"exitCode"`
}

func (c *Client) Extract(ctx context.Context, taskID, sevenZip, archive, destination, password string, onEvent func(Progress)) (ExtractResult, error) {
	var result ExtractResult
	params := map[string]any{
		"sevenZip":    sevenZip,
		"archive":     archive,
		"destination": destination,
	}
	if password != "" {
		params["password"] = password
	}
	err := c.CallTask(ctx, taskID, "extract", params, &result, onEvent)
	return result, err
}

type InstallTarget struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type InstallResult struct {
	Files      int64    `json:"files"`
	Bytes      int64    `json:"bytes"`
	TotalFiles int64    `json:"totalFiles"`
	Warnings   []string `json:"warnings"`
}

func (c *Client) InstallStaging(ctx context.Context, taskID, stagingDir string, targets []InstallTarget, onEvent func(Progress)) (InstallResult, error) {
	var result InstallResult
	params := map[string]any{"stagingDir": stagingDir, "targets": targets}
	err := c.CallTask(ctx, taskID, "install_staging", params, &result, onEvent)
	return result, err
}
