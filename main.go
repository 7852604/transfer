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
	password := os.Getenv("ACCESS_PASSWORD")
	cookieSecure := env("COOKIE_SECURE", "") == "1"
	backupHour := 4
	backupKeep := 7

	if password == "" {
		// 未配置密码时随机生成一个，仅用于本地体验；正式部署务必设置 ACCESS_PASSWORD
		password = api.RandToken()
		log.Printf("⚠ 未设置 ACCESS_PASSWORD，本次运行随机生成访问密码: %s", password)
	}

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

	bk := backup.New(st, dataDir, uploadsDir, backupKeep, backupHour)

	srv := api.New(api.Config{
		Password:     password,
		CookieSecure: cookieSecure,
		DataDir:      dataDir,
		UploadsDir:   uploadsDir,
		DBPath:       dbPath,
		MaxFileBytes: 50 << 20,
		MaxTextLen:   64 * 1024,
	}, st, bk)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	bk.Start(ctx)

	httpSrv := &http.Server{
		Addr:              ":" + port,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// 上传走流式 multipart，不用 MaxBytesReader 包整个 body；
		// 单文件大小由 saveUpload 里的 LimitReader 控制。
	}

	go func() {
		log.Printf("速传已启动: http://localhost:%s （数据目录 %s）", port, dataDir)
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
