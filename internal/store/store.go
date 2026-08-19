package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Message struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"` // "text" | "file"
	Content   string `json:"content"`
	FileID    string `json:"fileId,omitempty"`
	FileName  string `json:"fileName,omitempty"`
	FileSize  int64  `json:"fileSize,omitempty"`
	FileMime  string `json:"fileMime,omitempty"`
	IsImage   bool   `json:"isImage,omitempty"`
	CreatedAt int64  `json:"createdAt"` // unix 毫秒
}

var ErrNotFound = errors.New("not found")

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// 单用户量级很小，串行化连接可彻底避免 "database is locked"
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
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS messages (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	type       TEXT    NOT NULL CHECK (type IN ('text','file')),
	content    TEXT    NOT NULL DEFAULT '',
	file_id    TEXT,
	file_name  TEXT,
	file_size  INTEGER NOT NULL DEFAULT 0,
	file_mime  TEXT,
	is_image   INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_created ON messages(created_at);
CREATE TABLE IF NOT EXISTS tokens (
	token      TEXT PRIMARY KEY,
	created_at INTEGER NOT NULL
);`)
	return err
}

const cols = `id, type, content, file_id, file_name, file_size, file_mime, is_image, created_at`

type scanner interface{ Scan(dest ...any) error }

func scanMessage(row scanner) (Message, error) {
	var m Message
	var isImage int
	var fileID, fileName, fileMime sql.Null[string]
	err := row.Scan(&m.ID, &m.Type, &m.Content, &fileID, &fileName, &m.FileSize, &fileMime, &isImage, &m.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	if err != nil {
		return m, err
	}
	m.FileID, m.FileName, m.FileMime = fileID.V, fileName.V, fileMime.V
	m.IsImage = isImage == 1
	return m, nil
}

func (s *Store) InsertText(content string) (Message, error) {
	res, err := s.db.Exec(`INSERT INTO messages (type, content, created_at) VALUES ('text', ?, ?)`, content, nowMillis())
	if err != nil {
		return Message{}, err
	}
	id, _ := res.LastInsertId()
	return s.Get(id)
}

func (s *Store) InsertFile(fileID, fileName, fileMime string, size int64, isImage bool) (Message, error) {
	img := 0
	if isImage {
		img = 1
	}
	res, err := s.db.Exec(`INSERT INTO messages (type, content, file_id, file_name, file_size, file_mime, is_image, created_at)
		VALUES ('file', ?, ?, ?, ?, ?, ?, ?)`, fileName, fileID, fileName, size, fileMime, img, nowMillis())
	if err != nil {
		return Message{}, err
	}
	id, _ := res.LastInsertId()
	return s.Get(id)
}

func (s *Store) Get(id int64) (Message, error) {
	row := s.db.QueryRow(`SELECT `+cols+` FROM messages WHERE id = ?`, id)
	return scanMessage(row)
}

func (s *Store) GetByFileID(fileID string) (Message, error) {
	row := s.db.QueryRow(`SELECT `+cols+` FROM messages WHERE file_id = ? LIMIT 1`, fileID)
	return scanMessage(row)
}

// List 拉取消息：
//   - after > 0:  返回 id 大于 after 的消息（轮询增量），升序
//   - before > 0: 返回 id 小于 before 的最新一页（向上翻页），升序
//   - 其余:       返回最新一页，升序
//
// hasMore 表示是否还有更早的历史。
func (s *Store) List(after, before int64, limit int) ([]Message, bool, error) {
	var rows *sql.Rows
	var err error
	switch {
	case after > 0:
		rows, err = s.db.Query(`SELECT `+cols+` FROM messages WHERE id > ? ORDER BY id ASC LIMIT ?`, after, limit)
	case before > 0:
		rows, err = s.db.Query(`SELECT `+cols+` FROM messages WHERE id < ? ORDER BY id DESC LIMIT ?`, before, limit+1)
	default:
		rows, err = s.db.Query(`SELECT `+cols+` FROM messages ORDER BY id DESC LIMIT ?`, limit+1)
	}
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	// 初始化为空切片，保证 JSON 序列化为 [] 而不是 null
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
		// 查询是最新在前，反转为时间正序
		for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
			msgs[i], msgs[j] = msgs[j], msgs[i]
		}
	}
	return msgs, hasMore, nil
}

func (s *Store) Delete(id int64) (Message, error) {
	m, err := s.Get(id)
	if err != nil {
		return m, err
	}
	_, err = s.db.Exec(`DELETE FROM messages WHERE id = ?`, id)
	return m, err
}

// DeleteBefore 删除 created_at 早于 ts（unix 毫秒）的消息，返回被删除的列表供调用方清理文件。
func (s *Store) DeleteBefore(ts int64) ([]Message, error) {
	rows, err := s.db.Query(`SELECT `+cols+` FROM messages WHERE created_at < ?`, ts)
	if err != nil {
		return nil, err
	}
	var msgs []Message
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
	if _, err := s.db.Exec(`DELETE FROM messages WHERE created_at < ?`, ts); err != nil {
		return nil, err
	}
	return msgs, nil
}

func (s *Store) DeleteAll() ([]Message, error) {
	rows, err := s.db.Query(`SELECT ` + cols + ` FROM messages`)
	if err != nil {
		return nil, err
	}
	var msgs []Message
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
	if _, err := s.db.Exec(`DELETE FROM messages`); err != nil {
		return nil, err
	}
	return msgs, nil
}

// Search 模糊搜索：按空白拆成多个关键词，全部命中才返回（顺序无关）。
// "admin 密码" 能命中 "密码是 admin123" 这类内容。
func (s *Store) Search(q string, limit int) ([]Message, error) {
	keywords := strings.Fields(q)
	if len(keywords) == 0 {
		return []Message{}, nil
	}
	conds := make([]string, 0, len(keywords))
	args := make([]any, 0, len(keywords)+1)
	for _, kw := range keywords {
		esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(kw)
		conds = append(conds, `content LIKE '%' || ? || '%' ESCAPE '\'`)
		args = append(args, esc)
	}
	args = append(args, limit)
	rows, err := s.db.Query(`SELECT `+cols+` FROM messages WHERE `+strings.Join(conds, " AND ")+` ORDER BY id DESC LIMIT ?`, args...)
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

func (s *Store) Stats() (count int64, fileBytes int64, err error) {
	err = s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(file_size),0) FROM messages`).Scan(&count, &fileBytes)
	return
}

func (s *Store) CreateToken(token string) error {
	_, err := s.db.Exec(`INSERT INTO tokens (token, created_at) VALUES (?, ?)`, token, nowMillis())
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
