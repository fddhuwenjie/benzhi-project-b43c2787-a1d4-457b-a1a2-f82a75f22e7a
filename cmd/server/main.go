package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"astroplate-vault/internal/application"
	"astroplate-vault/internal/httpapi"
	"astroplate-vault/internal/persistence"
)

const defaultAddr = "127.0.0.1:19081"

type config struct {
	addr             string
	dataDir          string
	selfCheck        bool
	selfCheckTimeout time.Duration
}

func main() {
	if err := run(); err != nil {
		slog.Error("服务退出", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := parseConfig()
	if err != nil {
		return err
	}
	if cfg.selfCheck {
		return runSelfCheck(cfg)
	}
	if err = os.MkdirAll(cfg.dataDir, 0o750); err != nil {
		return fmt.Errorf("创建数据目录: %w", err)
	}
	store, err := persistence.Open(filepath.Join(cfg.dataDir, "astroplate-vault.db"))
	if err != nil {
		return fmt.Errorf("打开数据库: %w", err)
	}
	defer store.Close()
	if err = application.NewService(store).ValidateAuditChains(context.Background()); err != nil {
		return fmt.Errorf("启动审计校验失败: %w", err)
	}
	server := newServer(cfg.addr, store)
	listener, err := net.Listen("tcp", cfg.addr)
	if err != nil {
		return fmt.Errorf("监听 %s: %w", cfg.addr, err)
	}
	slog.Info("astroplate-vault 已启动", "addr", listener.Addr().String())
	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve(listener) }()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	select {
	case sig := <-signals:
		slog.Info("收到关闭信号", "signal", sig.String())
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(ctx)
	case err = <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func parseConfig() (config, error) {
	addr := flag.String("addr", defaultAddr, "HTTP 监听地址")
	dataDir := flag.String("data-dir", "./data", "SQLite 数据目录")
	selfCheck := flag.Bool("self-check", false, "执行真实 HTTP 全流程自检后退出")
	timeout := flag.Duration("self-check-timeout", 20*time.Second, "自检超时时间")
	flag.Parse()
	explicitAddr := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "addr" {
			explicitAddr = true
		}
	})
	resolved := *addr
	if port := os.Getenv("PORT"); port != "" && !explicitAddr {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return config{}, fmt.Errorf("PORT 必须是 1 到 65535 的端口号")
		}
		resolved = net.JoinHostPort("127.0.0.1", port)
	}
	if _, _, err := net.SplitHostPort(resolved); err != nil {
		return config{}, fmt.Errorf("无效的 -addr: %w", err)
	}
	if *timeout <= 0 {
		return config{}, fmt.Errorf("-self-check-timeout 必须大于 0")
	}
	return config{addr: resolved, dataDir: *dataDir, selfCheck: *selfCheck, selfCheckTimeout: *timeout}, nil
}

func newServer(addr string, store *persistence.Store) *http.Server {
	service := application.NewService(store)
	api := httpapi.New(service)
	return &http.Server{Addr: addr, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
}
