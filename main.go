package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"transfer/internal/api"
	"transfer/internal/backup"
	"transfer/internal/store"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	port := env("PORT", "8787")
	dataDir := env("DATA_DIR", "data")
	webhookToken := os.Getenv("WEBHOOK_TOKEN")
	adminPassword := os.Getenv("ADMIN_PASSWORD")
	cookieSecure := env("COOKIE_SECURE", "") == "1"
	trashRetainDays := 3

	uploadsDir := filepath.Join(dataDir, "uploads")
	for _, d := range []string{dataDir, uploadsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			log.Fatalf("创建目录 %s 失败: %v", d, err)
		}
	}
	dbPath := filepath.Join(dataDir, "transfer.db")

	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer st.Close()

	bk := backup.New(st, dataDir, uploadsDir, 7, 4)

	if webhookToken == "" {
		webhookToken = api.RandToken()
		log.Printf("⚠ 未设置 WEBHOOK_TOKEN，本次运行随机生成: %s", webhookToken)
	}

	srv := api.New(api.Config{
		DataDir:       dataDir,
		UploadsDir:    uploadsDir,
		DBPath:        dbPath,
		MaxFileBytes:  50 << 20,
		MaxTextLen:    64 * 1024,
		CookieSecure:  cookieSecure,
		WebhookToken:  webhookToken,
		AdminPassword: adminPassword,
	}, st, bk)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	bk.Start(ctx)

	// 回收站定时清理：每小时检查一次，删除超过 trashRetainDays 天的回收站消息
	go startTrashPurger(ctx, st, uploadsDir, trashRetainDays)

	httpSrv := &http.Server{
		Addr:              ":" + port,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("速传已启动: http://localhost:%s （数据目录 %s）", port, dataDir)
		log.Printf("BUG反馈 webhook token: %s", webhookToken)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP 服务退出: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("收到退出信号，正在关闭…")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
}

// startTrashPurger 每小时清理一次过期回收站消息
func startTrashPurger(ctx context.Context, st *store.Store, uploadsDir string, retainDays int) {
	purge := func() {
		msgs, err := st.PurgeExpired(retainDays)
		if err != nil {
			log.Printf("回收站清理失败: %v", err)
			return
		}
		for _, m := range msgs {
			if m.FileID != "" {
				os.Remove(filepath.Join(uploadsDir, m.FileID))
			}
		}
		if len(msgs) > 0 {
			log.Printf("回收站清理: 永久删除 %d 条过期消息（超过 %d 天）", len(msgs), retainDays)
		}
	}

	// 启动时先跑一次
	purge()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			purge()
		}
	}
}
