package api

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"transfer/internal/store"
)

const sniffLen = 512

// handleUpload 接收 multipart 文件（字段名 file），存入当前房间
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	room := getRoom(r)
	reader, err := r.MultipartReader()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "不是有效的文件上传请求"})
		return
	}
	var created []store.Message
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "读取上传数据失败"})
			return
		}
		if part.FormName() != "file" {
			part.Close()
			continue
		}
		msg, err := s.saveUploadToRoom(room.ID, part)
		part.Close()
		if err != nil {
			if he, ok := err.(*httpError); ok {
				writeJSON(w, he.status, map[string]any{"error": he.msg})
			} else {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "保存文件失败"})
			}
			return
		}
		created = append(created, *msg)
	}
	if len(created) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "没有收到文件"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": created})
}

type httpError struct {
	status int
	msg    string
}

func (e *httpError) Error() string { return e.msg }

// saveUploadToRoom 把上传的文件存入指定房间
func (s *Server) saveUploadToRoom(roomID int64, part *multipart.Part) (*store.Message, error) {
	origName := filepath.Base(part.FileName())
	if origName == "" || origName == "." || origName == "/" {
		origName = "unnamed"
	}

	ext := strings.ToLower(filepath.Ext(origName))
	if len(ext) > 10 || strings.ContainsAny(ext, `/\ :`) {
		ext = ""
	}
	fileID := fmt.Sprintf("%d-%s%s", time.Now().UnixMilli(), randHex(6), ext)

	dstPath := filepath.Join(s.cfg.UploadsDir, fileID)
	dst, err := os.Create(dstPath)
	if err != nil {
		return nil, err
	}
	defer dst.Close()

	head := make([]byte, sniffLen)
	n, _ := io.ReadFull(part, head)

	limited := io.LimitReader(part, s.cfg.MaxFileBytes+1)
	written, err := io.Copy(dst, io.MultiReader(bytes.NewReader(head[:n]), limited))
	if err != nil {
		os.Remove(dstPath)
		return nil, err
	}
	if written > s.cfg.MaxFileBytes {
		os.Remove(dstPath)
		return nil, &httpError{status: http.StatusRequestEntityTooLarge, msg: "文件超过大小上限"}
	}

	mimeType := part.Header.Get("Content-Type")
	if mimeType == "" || !strings.Contains(mimeType, "/") {
		mimeType = http.DetectContentType(head[:n])
	}
	isImage := strings.HasPrefix(mimeType, "image/")

	msg, err := s.store.InsertFile(roomID, fileID, origName, mimeType, written, isImage)
	if err != nil {
		os.Remove(dstPath)
		return nil, err
	}
	return &msg, nil
}

// handleServeFile 输出文件；图片/音视频 inline 预览，其余 attachment 下载。
// 只允许下载当前房间的文件——防止进了任意房间就能拿其他房间的文件。
func (s *Server) handleServeFile(w http.ResponseWriter, r *http.Request) {
	fileID := r.PathValue("fileId")
	if fileID == "" || strings.ContainsAny(fileID, `/\`) || strings.Contains(fileID, "..") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "非法文件名"})
		return
	}
	msg, err := s.store.GetByFileID(fileID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "文件不存在"})
		return
	}
	// 房间归属校验：不属于当前房间的文件一律 404（不暴露存在性）
	if room := getRoom(r); msg.RoomID != room.ID {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "文件不存在"})
		return
	}
	f, err := os.Open(filepath.Join(s.cfg.UploadsDir, fileID))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "文件不存在"})
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "文件不存在"})
		return
	}

	inline := msg.IsImage ||
		strings.HasPrefix(msg.FileMime, "video/") ||
		strings.HasPrefix(msg.FileMime, "audio/")
	disposition := "inline"
	if !inline || r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	name := msg.FileName
	if name == "" {
		name = fileID
	}
	if msg.FileMime != "" {
		w.Header().Set("Content-Type", msg.FileMime)
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}

// removeMessageFile 删除消息对应的落盘文件
func (s *Server) removeMessageFile(m store.Message) {
	if m.FileID == "" {
		return
	}
	_ = os.Remove(filepath.Join(s.cfg.UploadsDir, m.FileID))
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
