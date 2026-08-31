package localservice

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Manager owns only the child it starts; it never kills a process found on a port.
type Manager struct {
	mu            sync.Mutex
	cmd           *exec.Cmd
	done          chan struct{}
	lastError     string
	stopping      bool
	root          string
	shutdownToken string
}

func New() *Manager { root, _ := filepath.Abs("../doubao-web-api"); return &Manager{root: root} }
func (m *Manager) ready() bool {
	client := http.Client{Timeout: time.Second}
	res, err := client.Get("http://127.0.0.1:8086/health")
	if err != nil {
		return false
	}
	defer res.Body.Close()
	var v struct {
		Status      string `json:"status"`
		MaxParallel int    `json:"max_parallel"`
	}
	return res.StatusCode == 200 && json.NewDecoder(res.Body).Decode(&v) == nil && v.Status == "ok" && v.MaxParallel > 0
}
func (m *Manager) Status() gin.H {
	ready := m.ready()
	m.mu.Lock()
	defer m.mu.Unlock()
	state := "stopped"
	if m.cmd != nil {
		state = "starting"
	}
	if ready {
		state = "running"
	}
	if m.stopping {
		state = "stopping"
	}
	return gin.H{"state": state, "managed": m.cmd != nil, "ready": ready, "error": m.lastError, "adminUrl": "http://127.0.0.1:8086/admin", "logPath": filepath.Join(m.root, "data/service.log"), "storage": "local"}
}
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd != nil {
		return nil
	}
	conn, err := net.DialTimeout("tcp", "127.0.0.1:8086", time.Second)
	if err == nil {
		conn.Close()
		return fmt.Errorf("8086 端口已被占用；不会接管或停止已有服务")
	}
	binary := filepath.Join(m.root, "bin/doubao-web-api")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if _, err := os.Stat(binary); err != nil {
		return fmt.Errorf("请先运行 ./scripts/build.sh 构建豆包服务")
	}
	if err := os.MkdirAll(filepath.Join(m.root, "data"), 0700); err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(m.root, "data/service.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		log.Close()
		return err
	}
	m.shutdownToken = hex.EncodeToString(token)
	cmd := exec.Command(binary)
	cmd.Dir = m.root
	cmd.Stdout = log
	cmd.Stderr = log
	// Isolate the child from Novaly's PORT, database, cloud credentials and account paths.
	for _, e := range os.Environ() {
		k, _, _ := strings.Cut(e, "=")
		if strings.HasPrefix(k, "DOUBAO_") || strings.HasPrefix(k, "COS_") || k == "PORT" || k == "MAX_PARALLEL_VIDEO" {
			continue
		}
		cmd.Env = append(cmd.Env, e)
	}
	cmd.Env = append(cmd.Env, "DOUBAO_LOCAL_SHUTDOWN_TOKEN="+m.shutdownToken, "PORT=8086", "DOUBAO_CDP_PORT=9322", "DOUBAO_CDP_URL=http://127.0.0.1:9322", "MAX_PARALLEL_VIDEO=2", "DOUBAO_SESSION_DIR="+filepath.Join(m.root, "session"))
	if err := cmd.Start(); err != nil {
		log.Close()
		return err
	}
	m.cmd = cmd
	m.done = make(chan struct{})
	m.lastError = ""
	done := m.done
	go func() {
		err := cmd.Wait()
		log.Close()
		m.mu.Lock()
		if err != nil && !m.stopping {
			m.lastError = err.Error() + "；请查看豆包日志"
		}
		m.cmd = nil
		m.stopping = false
		close(done)
		m.mu.Unlock()
	}()
	return nil
}
func (m *Manager) Stop() error {
	m.mu.Lock()
	if m.cmd == nil {
		m.mu.Unlock()
		return nil
	}
	cmd, done, token := m.cmd, m.done, m.shutdownToken
	m.stopping = true
	m.mu.Unlock()
	var err error
	if runtime.GOOS == "windows" {
		req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:8086/internal/local-shutdown", nil)
		req.Header.Set("X-Novaly-Shutdown", token)
		client := http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}}
		var res *http.Response
		res, err = client.Do(req)
		if res != nil {
			res.Body.Close()
			if res.StatusCode != http.StatusAccepted {
				err = fmt.Errorf("停止服务失败 (HTTP %d)", res.StatusCode)
			}
		}
	} else {
		err = cmd.Process.Signal(syscall.SIGTERM)
	}
	if err != nil && err != os.ErrProcessDone {
		m.mu.Lock()
		m.stopping = false
		m.mu.Unlock()
		return fmt.Errorf("暂时无法停止豆包服务；若仍在启动，请等启动完成后重试: %w", err)
	}
	select {
	case <-done:
		return nil
	case <-time.After(30 * time.Second):
		return fmt.Errorf("豆包服务仍在退出，请稍后刷新；未强杀浏览器")
	}
}
func (m *Manager) Register(r *gin.Engine) {
	group := r.Group("/api/local/doubao", func(c *gin.Context) {
		// Reject browser cross-origin process control, including DNS-rebinding hosts.
		host, _, err := net.SplitHostPort(c.Request.Host)
		if err != nil {
			host = c.Request.Host
		}
		if host != "127.0.0.1" && host != "localhost" && host != "::1" {
			c.AbortWithStatus(403)
			return
		}
		if origin := c.GetHeader("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || (u.Host != c.Request.Host && u.Host != "127.0.0.1:5173" && u.Host != "localhost:5173") {
				c.AbortWithStatus(403)
				return
			}
		}
		if c.Request.Method == "POST" && c.GetHeader("X-Novaly-Local") != "1" {
			c.AbortWithStatus(403)
			return
		}
	})
	group.GET("", func(c *gin.Context) { c.JSON(200, m.Status()) })
	group.POST("/start", func(c *gin.Context) {
		if err := m.Start(); err != nil {
			c.JSON(409, gin.H{"error": err.Error()})
			return
		}
		c.JSON(202, m.Status())
	})
	group.POST("/stop", func(c *gin.Context) {
		if err := m.Stop(); err != nil {
			c.JSON(409, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, m.Status())
	})
}
