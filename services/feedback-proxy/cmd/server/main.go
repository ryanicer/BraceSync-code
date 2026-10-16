// Package main T709 运维反馈代理 service 启动入口（独立容器，Boss 2026-10-16 批 peter v3 设计）。
//
// 环境变量（容器 env only，凭据不入代码/配置仓/日志）：
//
//	PORT                 监听端口，默认 8087
//	TAPD_BASE_URL        默认 https://api.tapd.cn（白名单闸只放 https+tapd.cn 系）
//	TAPD_WORKSPACE_ID    默认 33005753（矫智通 BraceSync）
//	TAPD_API_USER        TAPD 服务账号 number（Basic Auth 前段）
//	TAPD_API_PASSWORD    TAPD 服务账号 token（Basic Auth 后段）
//	TAPD_ATTACHMENT_TYPE 缺陷附件 type 枚举（D-2 注：联调期实测；默认 bug_attachment）
//	TAPD_ALLOW_LOOPBACK  仅单测=1 时放行环回 fake TAPD（生产禁设）
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bracesync/bracesync/services/feedback-proxy/internal/proxy"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	port := envOr("PORT", "8087")
	base := envOr("TAPD_BASE_URL", "https://api.tapd.cn")
	workspace := envOr("TAPD_WORKSPACE_ID", "33005753")
	attachType := envOr("TAPD_ATTACHMENT_TYPE", "bug_attachment")
	user := os.Getenv("TAPD_API_USER")
	pass := os.Getenv("TAPD_API_PASSWORD")

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// 凭据 fail-closed：缺一不启动（避免半配置容器带病接单）。
	if strings.TrimSpace(user) == "" || strings.TrimSpace(pass) == "" {
		logger.Error("TAPD_API_USER / TAPD_API_PASSWORD missing (container env only)")
		os.Exit(1)
	}

	allowLoopback := os.Getenv("TAPD_ALLOW_LOOPBACK") == "1"
	client := proxy.NewTapdClient(base, user, pass, workspace, attachType)
	h, err := proxy.NewHandler(client, proxy.Page(), logger, allowLoopback)
	if err != nil {
		logger.Error("invalid configuration", "err", err.Error())
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       6 * time.Minute, // 大附件上传（150MB/张）给足读窗
		WriteTimeout:      6 * time.Minute,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("feedback-proxy listening", "port", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "err", err.Error())
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "shutdown:", err)
	}
}
