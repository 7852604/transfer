package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"transfer/internal/store"
)

// Manager 负责每日备份：VACUUM INTO 生成数据库快照，
// 再和 uploads 一起打成 tar.gz，保留最近 N 份。
type Manager struct {
	mu         sync.Mutex
	store      *store.Store
	dataDir    string // 数据库所在目录，备份临时文件也放这里
	uploadsDir string
	keep       int
	hour       int
}

func New(st *store.Store, dataDir, uploadsDir string, keep, hour int) *Manager {
	if keep < 1 {
		keep = 7
	}
	if hour < 0 || hour > 23 {
		hour = 4
	}
	return &Manager{store: st, dataDir: dataDir, uploadsDir: uploadsDir, keep: keep, hour: hour}
}

// Run 执行一次备份，返回备份文件名和大小。串行化，重入会等待。
func (m *Manager) Run() (name string, size int64, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	tmpDB := filepath.Join(m.dataDir, "backup-tmp.db")
	_ = os.Remove(tmpDB)
	if err := m.store.VacuumInto(tmpDB); err != nil {
		return "", 0, err
	}
	defer os.Remove(tmpDB)

	backupDir := filepath.Join(m.dataDir, "backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return "", 0, err
	}
	name = "transfer-" + time.Now().Format("20060102-150405") + ".tar.gz"
	outPath := filepath.Join(backupDir, name)

	out, err := os.Create(outPath)
	if err != nil {
		return "", 0, err
	}
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)

	add := func(src, arcName string) error {
		f, err := os.Open(src)
		if err != nil {
			return err
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(st, "")
		if err != nil {
			return err
		}
		hdr.Name = arcName
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		_, err = io.Copy(tw, f)
		return err
	}

	err = add(tmpDB, "backup.db")
	if err == nil {
		entries, walkErr := os.ReadDir(m.uploadsDir)
		if walkErr != nil && !os.IsNotExist(walkErr) {
			err = walkErr
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if err = add(filepath.Join(m.uploadsDir, e.Name()), "uploads/"+e.Name()); err != nil {
				break
			}
		}
	}

	tw.Close()
	gz.Close()
	out.Close()

	if err != nil {
		os.Remove(outPath)
		return "", 0, err
	}
	if st, statErr := os.Stat(outPath); statErr == nil {
		size = st.Size()
	}
	m.prune(backupDir)
	return name, size, nil
}

func (m *Manager) prune(backupDir string) {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "transfer-") && strings.HasSuffix(e.Name(), ".tar.gz") {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names))) // 新的在前
	if len(names) > m.keep {
		for _, n := range names[m.keep:] {
			_ = os.Remove(filepath.Join(backupDir, n))
		}
	}
}

// Last 返回最近一次备份的信息
func (m *Manager) Last() (name string, size int64, at time.Time, ok bool) {
	backupDir := filepath.Join(m.dataDir, "backups")
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return "", 0, time.Time{}, false
	}
	var newest string
	var newestMod time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "transfer-") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if newest == "" || info.ModTime().After(newestMod) {
			newest, newestMod = e.Name(), info.ModTime()
		}
	}
	if newest == "" {
		return "", 0, time.Time{}, false
	}
	info, err := os.Stat(filepath.Join(backupDir, newest))
	if err != nil {
		return "", 0, time.Time{}, false
	}
	return newest, info.Size(), newestMod, true
}

// Start 启动每日定时备份（默认凌晨 4 点，BACKUP_HOUR 可调）
func (m *Manager) Start(ctx context.Context) {
	go func() {
		for {
			now := time.Now()
			next := time.Date(now.Year(), now.Month(), now.Day(), m.hour, 0, 0, 0, now.Location())
			if !next.After(now) {
				next = next.Add(24 * time.Hour)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Until(next)):
				if _, _, err := m.Run(); err != nil {
					log.Printf("定时备份失败: %v", err)
				} else {
					log.Printf("定时备份完成，保留最近 %d 份", m.keep)
				}
			}
		}
	}()
}
