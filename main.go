package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed web/*
var webFiles embed.FS

type Config struct {
	RTMPPort  int    `json:"rtmp_port"`
	HLSPort   int    `json:"hls_port"`
	WebPort   int    `json:"web_port"`
	StreamKey string `json:"stream_key"`
	SRSBinary string `json:"srs_binary"`
}

type Paths struct {
	DataDir string
	Config  string
	SRS     string
	HLS     string
	Log     string
}

type MediaManager struct {
	mu      sync.Mutex
	cfg     Config
	paths   Paths
	binary  string
	cancel  context.CancelFunc
	done    chan struct{}
	running bool
	lastErr string
}

type App struct {
	cfg     Config
	manager *MediaManager
	proxy   *httputil.ReverseProxy
}

func main() {
	var dataDir string
	var configPath string
	var rtmpPort int
	var hlsPort int
	var webPort int
	var streamKey string
	var srsBinary string
	var openBrowser bool

	flag.StringVar(&dataDir, "data-dir", "./data", "directorio de datos")
	flag.StringVar(&configPath, "config", "", "archivo de configuración")
	flag.IntVar(&rtmpPort, "rtmp-port", 0, "puerto RTMP")
	flag.IntVar(&hlsPort, "hls-port", 0, "puerto HLS interno")
	flag.IntVar(&webPort, "web-port", 0, "puerto de la aplicación web")
	flag.StringVar(&streamKey, "stream-key", "", "stream key de la Osmo")
	flag.StringVar(&srsBinary, "srs", "", "ruta al ejecutable SRS")
	flag.BoolVar(&openBrowser, "open", true, "abrir la interfaz en el navegador")
	flag.Parse()

	cfg, paths, err := loadConfig(configPath, dataDir)
	if err != nil {
		log.Fatal(err)
	}
	if rtmpPort > 0 {
		cfg.RTMPPort = rtmpPort
	}
	if hlsPort > 0 {
		cfg.HLSPort = hlsPort
	}
	if webPort > 0 {
		cfg.WebPort = webPort
	}
	if strings.TrimSpace(streamKey) != "" {
		cfg.StreamKey = strings.TrimSpace(streamKey)
	}
	if strings.TrimSpace(srsBinary) != "" {
		cfg.SRSBinary = strings.TrimSpace(srsBinary)
	}
	if err := normalizeConfig(&cfg); err != nil {
		log.Fatal(err)
	}
	if err := saveConfig(paths.Config, cfg); err != nil {
		log.Fatal(err)
	}

	binary := findSRS(cfg.SRSBinary, paths.DataDir)
	manager := &MediaManager{cfg: cfg, paths: paths, binary: binary}
	app := &App{cfg: cfg, manager: manager}
	app.proxy = newHLSProxy(cfg.HLSPort)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := manager.Start(); err != nil {
		log.Printf("SRS no está iniciado: %v", err)
	}

	server := &http.Server{
		Addr:              net.JoinHostPort("127.0.0.1", fmt.Sprint(cfg.WebPort)),
		Handler:           app.handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		manager.Stop()
		log.Fatalf("no se pudo abrir la web en %s: %v", server.Addr, err)
	}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("servidor web detenido: %v", err)
		}
	}()

	fmt.Printf("RTMP Web\n")
	fmt.Printf("Web:       http://127.0.0.1:%d\n", cfg.WebPort)
	host := localIP()
	if host == "" {
		host = "<IP_DEL_PC>"
	}
	fmt.Printf("Osmo:      rtmp://%s:%d/live\n", host, cfg.RTMPPort)
	fmt.Printf("Stream key: %s\n", cfg.StreamKey)
	fmt.Printf("HLS web:   http://127.0.0.1:%d/hls/live/%s.m3u8\n", cfg.WebPort, cfg.StreamKey)
	fmt.Printf("OBS/VLC:   rtmp://%s:%d/live/%s\n", host, cfg.RTMPPort, cfg.StreamKey)
	if binary == "" {
		fmt.Println("SRS no encontrado. Instálalo o usa --srs /ruta/a/srs.")
	}

	if openBrowser {
		go func() {
			time.Sleep(300 * time.Millisecond)
			_ = openInBrowser(fmt.Sprintf("http://127.0.0.1:%d", cfg.WebPort))
		}()
	}

	<-ctx.Done()
	manager.Stop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}

func (a *App) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/hls/", http.StripPrefix("/hls", a.proxy))
	mux.HandleFunc("/api/status", a.handleStatus)
	mux.HandleFunc("/api/stream-info", a.handleStreamInfo)
	mux.HandleFunc("/api/start", a.handleStart)
	mux.HandleFunc("/api/stop", a.handleStop)
	mux.Handle("/", a.webHandler())
	return mux
}

func (a *App) webHandler() http.Handler {
	fsys, err := fs.Sub(webFiles, "web")
	if err != nil {
		return http.NotFoundHandler()
	}
	return http.FileServer(http.FS(fsys))
}

func (a *App) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	host := localIP()
	if host == "" {
		host = "<IP_DEL_PC>"
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"running":    a.manager.isRunning(),
		"error":      a.manager.getError(),
		"stream_key": a.cfg.StreamKey,
		"local_ip":   localIP(),
		"ingest_url": fmt.Sprintf("rtmp://%s:%d/live/%s", host, a.cfg.RTMPPort, a.cfg.StreamKey),
		"hls_url":    fmt.Sprintf("http://127.0.0.1:%d/hls/live/%s.m3u8", a.cfg.WebPort, a.cfg.StreamKey),
		"rtmp_play":  fmt.Sprintf("rtmp://%s:%d/live/%s", host, a.cfg.RTMPPort, a.cfg.StreamKey),
		"srs_binary": a.manager.binaryPath(),
	})
}

func (a *App) handleStreamInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	key := a.cfg.StreamKey
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/v1/streams/", a.cfg.HLSPort))
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
		return
	}
	streams, ok := data["streams"].([]any)
	if !ok {
		_ = json.NewEncoder(w).Encode(map[string]any{})
		return
	}
	for _, s := range streams {
		stream, ok := s.(map[string]any)
		if !ok {
			continue
		}
		name, _ := stream["name"].(string)
		if name != key {
			continue
		}
		vcodec := ""
		acodec := ""
		width := 0
		height := 0
		fps := 0.0
		bitrate := 0
		if v, ok := stream["video"].(map[string]any); ok {
			if c, ok := v["codec"].(string); ok {
				vcodec = c
			}
			if w, ok := v["width"].(float64); ok {
				width = int(w)
			}
			if h, ok := v["height"].(float64); ok {
				height = int(h)
			}
			if f, ok := v["fps"].(float64); ok {
				fps = f
			}
			if b, ok := v["bitrate"].(float64); ok {
				bitrate = int(b)
			}
		}
		if a, ok := stream["audio"].(map[string]any); ok {
			if c, ok := a["codec"].(string); ok {
				acodec = c
			}
		}
		resolution := fmt.Sprintf("%dx%d", width, height)
		if width == 0 || height == 0 {
			resolution = "—"
		}
		fpsStr := "—"
		if fps > 0 {
			fpsStr = fmt.Sprintf("%.0f", fps)
		}
		bitrateStr := "—"
		if bitrate > 0 {
			bitrateStr = fmt.Sprintf("%.1f Mbps", float64(bitrate)/1e6)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resolution": resolution,
			"fps":        fpsStr,
			"vcodec":     vcodec,
			"acodec":     acodec,
			"bitrate":    bitrateStr,
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{})
}

func (a *App) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := a.manager.Start(); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (a *App) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	a.manager.Stop()
	w.WriteHeader(http.StatusOK)
}

func newHLSProxy(port int) *httputil.ReverseProxy {
	target, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Director = func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.Header.Set("Accept-Encoding", "identity")
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		if resp.Request != nil && strings.HasSuffix(resp.Request.URL.Path, ".m3u8") {
			resp.Header.Set("Cache-Control", "no-store")
		}
		return nil
	}
	return proxy
}

func (m *MediaManager) Start() error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return nil
	}
	if m.binary == "" {
		m.binary = findSRS(m.cfg.SRSBinary, m.paths.DataDir)
	}
	if m.binary == "" {
		m.lastErr = "SRS no encontrado"
		m.mu.Unlock()
		return errors.New("SRS no encontrado")
	}
	if err := os.MkdirAll(m.paths.DataDir, 0o700); err != nil {
		m.lastErr = err.Error()
		m.mu.Unlock()
		return err
	}
	if err := os.MkdirAll(m.paths.HLS, 0o700); err != nil {
		m.lastErr = err.Error()
		m.mu.Unlock()
		return err
	}
	if err := os.WriteFile(m.paths.SRS, []byte(renderSRS(m.cfg, m.paths)), 0o600); err != nil {
		m.lastErr = err.Error()
		m.mu.Unlock()
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	m.cancel = cancel
	m.done = done
	m.running = true
	m.lastErr = ""
	m.mu.Unlock()
	go m.run(ctx, done)
	return nil
}

func (m *MediaManager) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		if ctx.Err() != nil {
			return
		}
		file, err := os.OpenFile(m.paths.Log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			m.setError(err)
			time.Sleep(2 * time.Second)
			continue
		}
		cmd := exec.CommandContext(ctx, m.binary, "-c", m.paths.SRS)
		cmd.Dir = m.paths.DataDir
		cmd.Stdout = file
		cmd.Stderr = file
		err = cmd.Run()
		_ = file.Close()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			m.setError(err)
		}
		time.Sleep(2 * time.Second)
	}
}

func (m *MediaManager) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	cancel := m.cancel
	done := m.done
	m.running = false
	m.cancel = nil
	m.lastErr = ""
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (m *MediaManager) binaryPath() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.binary
}

func (m *MediaManager) isRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

func (m *MediaManager) getError() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastErr
}

func (m *MediaManager) setError(err error) {
	m.mu.Lock()
	m.lastErr = err.Error()
	m.mu.Unlock()
}

func loadConfig(configPath, dataDir string) (Config, Paths, error) {
	if strings.TrimSpace(dataDir) == "" {
		dataDir = "./data"
	}
	absoluteData, err := filepath.Abs(dataDir)
	if err != nil {
		return Config{}, Paths{}, err
	}
	if strings.TrimSpace(configPath) == "" {
		configPath = filepath.Join(absoluteData, "config.json")
	} else if absolute, err := filepath.Abs(configPath); err == nil {
		configPath = absolute
	}
	paths := Paths{
		DataDir: absoluteData,
		Config:  configPath,
		SRS:     filepath.Join(absoluteData, "srs.conf"),
		HLS:     filepath.Join(absoluteData, "hls"),
		Log:     filepath.Join(absoluteData, "srs.log"),
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return Config{}, Paths{}, err
	}
	cfg := Config{RTMPPort: 1935, HLSPort: 8080, WebPort: 17890, StreamKey: randomKey()}
	data, err := os.ReadFile(configPath)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, paths, nil
	}
	if err != nil {
		return Config{}, Paths{}, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, Paths{}, err
	}
	if strings.TrimSpace(cfg.StreamKey) == "" {
		cfg.StreamKey = randomKey()
	}
	if err := normalizeConfig(&cfg); err != nil {
		return Config{}, Paths{}, err
	}
	return cfg, paths, nil
}

func normalizeConfig(cfg *Config) error {
	if cfg.RTMPPort == 0 {
		cfg.RTMPPort = 1935
	}
	if cfg.HLSPort == 0 {
		cfg.HLSPort = 8080
	}
	if cfg.WebPort == 0 {
		cfg.WebPort = 17890
	}
	for _, item := range []struct {
		name string
		port int
	}{
		{"RTMP", cfg.RTMPPort},
		{"HLS", cfg.HLSPort},
		{"web", cfg.WebPort},
	} {
		if item.port < 1 || item.port > 65535 {
			return fmt.Errorf("puerto %s inválido: %d", item.name, item.port)
		}
	}
	if strings.TrimSpace(cfg.StreamKey) == "" {
		cfg.StreamKey = randomKey()
	}
	return nil
}

func saveConfig(path string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func randomKey() string {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "juramento-local"
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func findSRS(configured, dataDir string) string {
	candidates := []string{}
	if configured != "" {
		candidates = append(candidates, configured)
		if !filepath.IsAbs(configured) {
			candidates = append([]string{filepath.Join(dataDir, configured)}, candidates...)
		}
	}
	candidates = append(candidates,
		filepath.Join(dataDir, "runtime", "srs", "srs"),
		filepath.Join(dataDir, "runtime", "srs", "srs.exe"),
	)
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	if found, err := exec.LookPath("srs"); err == nil {
		return found
	}
	return ""
}

func renderSRS(cfg Config, paths Paths) string {
	return fmt.Sprintf(`daemon off;
work_dir %q;
pid %q;
listen 0.0.0.0:%d;
srs_log_tank console;
srs_log_level warn;
http_server {
    enabled on;
    listen 127.0.0.1:%d;
    dir %q;
    crossdomain on;
}
vhost __defaultVhost__ {
    min_latency on;
    tcp_nodelay on;
    hls {
        enabled on;
        hls_path %q;
        hls_fragment 2;
        hls_window 10;
        hls_cleanup on;
        hls_dispose 60;
        hls_wait_keyframe on;
        hls_td_ratio 1.0;
        hls_entry_prefix /hls;
        hls_ctx off;
        hls_ts_ctx off;
        hls_keys off;
        hls_acodec aac;
        hls_vcodec h264;
    }
}
`, paths.DataDir, filepath.Join(paths.DataDir, "srs.pid"), cfg.RTMPPort, cfg.HLSPort, paths.HLS, paths.HLS)
}

func localIP() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	fallback := ""
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			var ip net.IP
			switch value := address.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ipv4 := ip.To4()
			if ipv4 == nil {
				continue
			}
			text := ipv4.String()
			if strings.HasPrefix(text, "192.168.") || strings.HasPrefix(text, "10.") {
				return text
			}
			if fallback == "" {
				fallback = text
			}
		}
	}
	return fallback
}

func openInBrowser(target string) error {
	var command string
	var args []string
	switch runtime.GOOS {
	case "windows":
		command = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", target}
	case "darwin":
		command = "open"
		args = []string{target}
	default:
		command = "xdg-open"
		args = []string{target}
	}
	return exec.Command(command, args...).Start()
}
