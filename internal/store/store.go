package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Room struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	HasPassword bool   `json:"hasPassword"`
	CreatedAt   int64  `json:"createdAt"`
	PinnedMsgID int64  `json:"pinnedMsgId"`
}

type Message struct {
	ID        int64  `json:"id"`
	RoomID    int64  `json:"roomId"`
	Type      string `json:"type"` // "text" | "file"
	Content   string `json:"content"`
	FileID    string `json:"fileId,omitempty"`
	FileName  string `json:"fileName,omitempty"`
	FileSize  int64  `json:"fileSize,omitempty"`
	FileMime  string `json:"fileMime,omitempty"`
	IsImage   bool   `json:"isImage,omitempty"`
	CreatedAt int64  `json:"createdAt"`
	Deleted   bool   `json:"-"` // 是否在回收站（内部用）
}

var ErrNotFound = errors.New("not found")

// hashPassword 存摘要不存明文。即使数据库泄露也拿不到原始密码。
func hashPassword(pw string) string {
	if pw == "" {
		return ""
	}
	h := sha256.Sum256([]byte(pw))
	return hex.EncodeToString(h[:])
}

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	// 迁移：如果旧表没有 room_id/deleted_at 列，用 ALTER TABLE 补上
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS rooms (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	name           TEXT    NOT NULL UNIQUE,
	password       TEXT    NOT NULL DEFAULT '',
	created_at     INTEGER NOT NULL,
	pinned_msg_id  INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS messages (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	room_id    INTEGER NOT NULL DEFAULT 1,
	type       TEXT    NOT NULL CHECK (type IN ('text','file')),
	content    TEXT    NOT NULL DEFAULT '',
	file_id    TEXT,
	file_name  TEXT,
	file_size  INTEGER NOT NULL DEFAULT 0,
	file_mime  TEXT,
	is_image   INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL,
	deleted_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_messages_room ON messages(room_id, deleted_at, id);
CREATE TABLE IF NOT EXISTS tokens (
	token      TEXT PRIMARY KEY,
	room_id    INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);`)
	if err != nil {
		return err
	}
	// 补列（旧库迁移，SQLite 的 ALTER TABLE ADD COLUMN 幂等性靠忽略错误实现）
	for _, col := range []string{"room_id", "deleted_at"} {
		s.db.Exec(fmt.Sprintf(`ALTER TABLE messages ADD COLUMN %s INTEGER NOT NULL DEFAULT 0`, col))
	}
	for _, col := range []string{"room_id"} {
		s.db.Exec(fmt.Sprintf(`ALTER TABLE tokens ADD COLUMN %s INTEGER NOT NULL DEFAULT 0`, col))
	}
	s.db.Exec(`ALTER TABLE rooms ADD COLUMN pinned_msg_id INTEGER NOT NULL DEFAULT 0`)
	// 确保默认房间存在
	s.db.Exec(`INSERT OR IGNORE INTO rooms (id, name, password, created_at) VALUES (1, 'BUG反馈', '', ?)`, nowMillis())
	return nil
}

const msgCols = `id, room_id, type, content, file_id, file_name, file_size, file_mime, is_image, created_at, deleted_at`

type scanner interface{ Scan(dest ...any) error }

func scanMessage(row scanner) (Message, error) {
	var m Message
	var isImage int
	var fileID, fileName, fileMime sql.Null[string]
	var deletedAt int64
	err := row.Scan(&m.ID, &m.RoomID, &m.Type, &m.Content, &fileID, &fileName, &m.FileSize, &fileMime, &isImage, &m.CreatedAt, &deletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	if err != nil {
		return m, err
	}
	m.FileID, m.FileName, m.FileMime = fileID.V, fileName.V, fileMime.V
	m.IsImage = isImage == 1
	m.Deleted = deletedAt > 0
	return m, nil
}

// ---------- 房间 ----------

const roomCols = `id, name, CASE WHEN password = '' THEN 0 ELSE 1 END, created_at, pinned_msg_id`

func (s *Store) ListRooms() ([]Room, error) {
	rows, err := s.db.Query(`SELECT ` + roomCols + ` FROM rooms ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rooms := []Room{}
	for rows.Next() {
		var r Room
		if err := rows.Scan(&r.ID, &r.Name, &r.HasPassword, &r.CreatedAt, &r.PinnedMsgID); err != nil {
			return nil, err
		}
		rooms = append(rooms, r)
	}
	return rooms, rows.Err()
}

func (s *Store) GetRoom(name string) (Room, error) {
	var r Room
	err := s.db.QueryRow(`SELECT `+roomCols+` FROM rooms WHERE name = ?`, name).Scan(&r.ID, &r.Name, &r.HasPassword, &r.CreatedAt, &r.PinnedMsgID)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

func (s *Store) GetRoomByID(id int64) (Room, error) {
	var r Room
	err := s.db.QueryRow(`SELECT `+roomCols+` FROM rooms WHERE id = ?`, id).Scan(&r.ID, &r.Name, &r.HasPassword, &r.CreatedAt, &r.PinnedMsgID)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

func (s *Store) CreateRoom(name, password string) (Room, error) {
	res, err := s.db.Exec(`INSERT INTO rooms (name, password, created_at) VALUES (?, ?, ?)`, name, hashPassword(password), nowMillis())
	if err != nil {
		return Room{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetRoomByID(id)
}

func (s *Store) DeleteRoom(id int64) ([]Message, error) {
	// 不允许删除默认房间
	if id == 1 {
		return nil, errors.New("不能删除默认房间")
	}
	// 查出该房间的所有消息（含已删除的），供清理文件
	rows, err := s.db.Query(`SELECT `+msgCols+` FROM messages WHERE room_id = ?`, id)
	if err != nil {
		return nil, err
	}
	msgs := []Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		msgs = append(msgs, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// 删消息和房间
	if _, err := s.db.Exec(`DELETE FROM messages WHERE room_id = ?`, id); err != nil {
		return nil, err
	}
	if _, err := s.db.Exec(`DELETE FROM tokens WHERE room_id = ?`, id); err != nil {
		return nil, err
	}
	if _, err := s.db.Exec(`DELETE FROM rooms WHERE id = ?`, id); err != nil {
		return nil, err
	}
	return msgs, nil
}

func (s *Store) CheckRoomPassword(id int64, password string) (bool, error) {
	var stored string
	err := s.db.QueryRow(`SELECT password FROM rooms WHERE id = ?`, id).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	// 兼容历史明文：若存量是哈希（64 位 hex）则比对哈希，否则按明文比对（仅过渡期）
	if len(stored) == 64 && stored == hashPassword(password) {
		return true, nil
	}
	return stored == password, nil
}

// SetRoomPassword 设置/修改/清除房间密码。password 为空则取消加密。
func (s *Store) SetRoomPassword(id int64, password string) error {
	_, err := s.db.Exec(`UPDATE rooms SET password = ? WHERE id = ?`, hashPassword(password), id)
	return err
}

// PinMessage 置顶消息到房间。msgID 为 0 表示取消置顶。
func (s *Store) PinMessage(roomID, msgID int64) error {
	_, err := s.db.Exec(`UPDATE rooms SET pinned_msg_id = ? WHERE id = ?`, msgID, roomID)
	return err
}

// ClearPinIfPinned 若 msgID 是 roomID 的置顶消息则解除置顶，返回是否解除过。
func (s *Store) ClearPinIfPinned(roomID, msgID int64) (bool, error) {
	res, err := s.db.Exec(`UPDATE rooms SET pinned_msg_id = 0 WHERE id = ? AND pinned_msg_id = ?`, roomID, msgID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ---------- 消息 ----------

func (s *Store) InsertText(roomID int64, content string) (Message, error) {
	res, err := s.db.Exec(`INSERT INTO messages (room_id, type, content, created_at) VALUES (?, 'text', ?, ?)`, roomID, content, nowMillis())
	if err != nil {
		return Message{}, err
	}
	id, _ := res.LastInsertId()
	return s.Get(id)
}

func (s *Store) InsertFile(roomID int64, fileID, fileName, fileMime string, size int64, isImage bool) (Message, error) {
	img := 0
	if isImage {
		img = 1
	}
	res, err := s.db.Exec(`INSERT INTO messages (room_id, type, content, file_id, file_name, file_size, file_mime, is_image, created_at)
		VALUES (?, 'file', ?, ?, ?, ?, ?, ?, ?)`, roomID, fileName, fileID, fileName, size, fileMime, img, nowMillis())
	if err != nil {
		return Message{}, err
	}
	id, _ := res.LastInsertId()
	return s.Get(id)
}

func (s *Store) Get(id int64) (Message, error) {
	row := s.db.QueryRow(`SELECT `+msgCols+` FROM messages WHERE id = ? AND deleted_at = 0`, id)
	return scanMessage(row)
}

func (s *Store) GetByFileID(fileID string) (Message, error) {
	row := s.db.QueryRow(`SELECT `+msgCols+` FROM messages WHERE file_id = ? LIMIT 1`, fileID)
	return scanMessage(row)
}

// List 拉取某房间的未删除消息
func (s *Store) List(roomID, after, before int64, limit int) ([]Message, bool, error) {
	var rows *sql.Rows
	var err error
	switch {
	case after > 0:
		rows, err = s.db.Query(`SELECT `+msgCols+` FROM messages WHERE room_id = ? AND deleted_at = 0 AND id > ? ORDER BY id ASC LIMIT ?`, roomID, after, limit)
	case before > 0:
		rows, err = s.db.Query(`SELECT `+msgCols+` FROM messages WHERE room_id = ? AND deleted_at = 0 AND id < ? ORDER BY id DESC LIMIT ?`, roomID, before, limit+1)
	default:
		rows, err = s.db.Query(`SELECT `+msgCols+` FROM messages WHERE room_id = ? AND deleted_at = 0 ORDER BY id DESC LIMIT ?`, roomID, limit+1)
	}
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	msgs := []Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, false, err
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}

	hasMore := false
	if after == 0 {
		if len(msgs) > limit {
			hasMore = true
			msgs = msgs[:limit]
		}
		for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
			msgs[i], msgs[j] = msgs[j], msgs[i]
		}
	}
	return msgs, hasMore, nil
}

// SoftDelete 软删除：标记 deleted_at，消息进入回收站。若被置顶则同时解除置顶。
func (s *Store) SoftDelete(id int64) (Message, error) {
	m, err := s.Get(id)
	if err != nil {
		return m, err
	}
	_, err = s.db.Exec(`UPDATE messages SET deleted_at = ? WHERE id = ?`, nowMillis(), id)
	if err != nil {
		return m, err
	}
	s.ClearPinIfPinned(m.RoomID, id)
	return m, nil
}

// Restore 从回收站恢复消息
func (s *Store) Restore(id int64) (Message, error) {
	row := s.db.QueryRow(`SELECT `+msgCols+` FROM messages WHERE id = ?`, id)
	m, err := scanMessage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	if err != nil {
		return m, err
	}
	_, err = s.db.Exec(`UPDATE messages SET deleted_at = 0 WHERE id = ?`, id)
	if err != nil {
		return m, err
	}
	// 重新查一次拿到 deleted_at=0 的状态
	row2 := s.db.QueryRow(`SELECT `+msgCols+` FROM messages WHERE id = ?`, id)
	return scanMessage(row2)
}

func (s *Store) GetByFileIDRaw(id int64) (Message, error) {
	row := s.db.QueryRow(`SELECT `+msgCols+` FROM messages WHERE id = ?`, id)
	return scanMessage(row)
}

// PermanentDelete 永久删除单条消息（从回收站清除）。若被置顶则同时解除置顶。
func (s *Store) PermanentDelete(id int64) (Message, error) {
	m, err := s.GetByFileIDRaw(id)
	if err != nil {
		return m, err
	}
	_, err = s.db.Exec(`DELETE FROM messages WHERE id = ?`, id)
	if err != nil {
		return m, err
	}
	s.ClearPinIfPinned(m.RoomID, id)
	return m, nil
}

// ListTrash 列出某房间的回收站消息
func (s *Store) ListTrash(roomID int64) ([]Message, error) {
	rows, err := s.db.Query(`SELECT `+msgCols+` FROM messages WHERE room_id = ? AND deleted_at > 0 ORDER BY deleted_at DESC`, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	msgs := []Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// PurgeExpired 永久删除超过 retainDays 天的回收站消息，返回被删除的列表供清理文件
func (s *Store) PurgeExpired(retainDays int) ([]Message, error) {
	cutoff := time.Now().AddDate(0, 0, -retainDays).UnixMilli()
	rows, err := s.db.Query(`SELECT `+msgCols+` FROM messages WHERE deleted_at > 0 AND deleted_at < ?`, cutoff)
	if err != nil {
		return nil, err
	}
	msgs := []Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		msgs = append(msgs, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(msgs) > 0 {
		if _, err := s.db.Exec(`DELETE FROM messages WHERE deleted_at > 0 AND deleted_at < ?`, cutoff); err != nil {
			return nil, err
		}
	}
	return msgs, nil
}

// DeleteBefore 清理某房间 N 天前的消息（含文件），返回被删除列表
func (s *Store) DeleteBefore(roomID, ts int64) ([]Message, error) {
	rows, err := s.db.Query(`SELECT `+msgCols+` FROM messages WHERE room_id = ? AND deleted_at = 0 AND created_at < ?`, roomID, ts)
	if err != nil {
		return nil, err
	}
	msgs := []Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		msgs = append(msgs, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if _, err := s.db.Exec(`DELETE FROM messages WHERE room_id = ? AND deleted_at = 0 AND created_at < ?`, roomID, ts); err != nil {
		return nil, err
	}
	s.PinMessage(roomID, 0) // 置顶消息可能已被清掉，直接解除
	return msgs, nil
}

// DeleteAll 清空某房间所有消息
func (s *Store) DeleteAll(roomID int64) ([]Message, error) {
	rows, err := s.db.Query(`SELECT ` + msgCols + ` FROM messages WHERE room_id = ? AND deleted_at = 0`, roomID)
	if err != nil {
		return nil, err
	}
	msgs := []Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		msgs = append(msgs, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if _, err := s.db.Exec(`DELETE FROM messages WHERE room_id = ? AND deleted_at = 0`, roomID); err != nil {
		return nil, err
	}
	s.PinMessage(roomID, 0) // 消息全清了，置顶一并解除
	return msgs, nil
}

// EmptyTrash 清空某房间回收站
func (s *Store) EmptyTrash(roomID int64) ([]Message, error) {
	rows, err := s.db.Query(`SELECT `+msgCols+` FROM messages WHERE room_id = ? AND deleted_at > 0`, roomID)
	if err != nil {
		return nil, err
	}
	msgs := []Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		msgs = append(msgs, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if _, err := s.db.Exec(`DELETE FROM messages WHERE room_id = ? AND deleted_at > 0`, roomID); err != nil {
		return nil, err
	}
	return msgs, nil
}

func (s *Store) Search(roomID int64, q string, limit int) ([]Message, error) {
	keywords := strings.Fields(q)
	if len(keywords) == 0 {
		return []Message{}, nil
	}
	conds := make([]string, 0, len(keywords))
	args := make([]any, 0, len(keywords)+2)
	args = append(args, roomID)
	for _, kw := range keywords {
		esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(kw)
		conds = append(conds, `content LIKE '%' || ? || '%' ESCAPE '\'`)
		args = append(args, esc)
	}
	args = append(args, limit)
	rows, err := s.db.Query(`SELECT `+msgCols+` FROM messages WHERE room_id = ? AND deleted_at = 0 AND (`+strings.Join(conds, " AND ")+`) ORDER BY id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	msgs := []Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func (s *Store) Stats(roomID int64) (count int64, fileBytes int64, trashCount int64, err error) {
	err = s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(file_size),0), (SELECT COUNT(*) FROM messages WHERE room_id = ? AND deleted_at > 0) FROM messages WHERE room_id = ? AND deleted_at = 0`, roomID, roomID).Scan(&count, &fileBytes, &trashCount)
	return
}

func (s *Store) StatsAll() (count int64, fileBytes int64, err error) {
	err = s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(file_size),0) FROM messages WHERE deleted_at = 0`).Scan(&count, &fileBytes)
	return
}

// ---------- Token ----------

func (s *Store) CreateToken(token string, roomID int64) error {
	_, err := s.db.Exec(`INSERT INTO tokens (token, room_id, created_at) VALUES (?, ?, ?)`, token, roomID, nowMillis())
	return err
}

func (s *Store) TokenExists(token string) (bool, error) {
	var one int
	err := s.db.QueryRow(`SELECT 1 FROM tokens WHERE token = ?`, token).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) DeleteToken(token string) error {
	_, err := s.db.Exec(`DELETE FROM tokens WHERE token = ?`, token)
	return err
}

// VacuumInto 生成一致性快照，运行中可安全调用。
func (s *Store) VacuumInto(destPath string) error {
	safe := strings.ReplaceAll(destPath, `'`, `''`)
	_, err := s.db.Exec(fmt.Sprintf(`VACUUM INTO '%s'`, safe))
	return err
}

func nowMillis() int64 { return time.Now().UnixMilli() }
