// Package mem 实现业务层会话持久化（JSONL），与 Eino 文档第三章思路一致：框架不负责存储，由业务读写消息再交给 Runner。
package mem

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
)

const sessionFileExt = ".jsonl"

type sessionHeader struct {
	Type      string    `json:"type"`
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

// Store 管理多个会话文件。
type Store struct {
	dir   string
	mu    sync.Mutex
	cache map[string]*Session
}

// NewStore 创建存储目录（若不存在）。
func NewStore(dir string) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("mem: 存储目录为空")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{
		dir:   dir,
		cache: make(map[string]*Session),
	}, nil
}

// Session 表示一次多轮对话；消息与磁盘 JSONL 同步。
type Session struct {
	store *Store

	id        string
	path      string
	createdAt time.Time
	msgs      []*schema.Message
	mu        sync.Mutex
}

// GetOrCreate 加载或新建会话。sessionID 为空时新建并分配 UUID；非空时须已有对应 .jsonl，否则报错。
// 第二个返回值为是否本次新建了会话文件（仅 sessionID 为空时为 true；恢复已有会话为 false）。
func (s *Store) GetOrCreate(sessionID string) (*Session, bool, error) {
	id := strings.TrimSpace(sessionID)
	created := false
	if id == "" {
		id = uuid.New().String()
		created = true
	}

	s.mu.Lock()
	if ex, ok := s.cache[id]; ok {
		s.mu.Unlock()
		return ex, false, nil
	}
	s.mu.Unlock()

	path := filepath.Join(s.dir, id+sessionFileExt)
	if created {
		sess, err := s.newSessionFile(id, path)
		if err != nil {
			return nil, false, err
		}
		return sess, true, nil
	}

	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, false, fmt.Errorf("会话不存在: %s", id)
		}
		return nil, false, err
	}
	sess, err := s.loadSession(id, path)
	if err != nil {
		return nil, false, err
	}
	return sess, false, nil
}

func (s *Store) newSessionFile(id, path string) (*Session, error) {
	createdAt := time.Now().UTC()
	hdr := sessionHeader{Type: "session", ID: id, CreatedAt: createdAt}
	b, err := json.Marshal(hdr)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		return nil, err
	}
	sess := &Session{
		store:     s,
		id:        id,
		path:      path,
		createdAt: createdAt,
		msgs:      nil,
	}
	s.mu.Lock()
	s.cache[id] = sess
	s.mu.Unlock()
	return sess, nil
}

func (s *Store) loadSession(id, path string) (*Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	rawBuf := make([]byte, 0, 64*1024)
	sc.Buffer(rawBuf, 4*1024*1024)

	var hdr sessionHeader
	var msgs []*schema.Message
	lineNum := 0
	for sc.Scan() {
		lineNum++
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		if lineNum == 1 {
			if err := json.Unmarshal(line, &hdr); err != nil {
				return nil, fmt.Errorf("会话头解析: %w", err)
			}
			if hdr.Type != "session" || hdr.ID == "" {
				return nil, errors.New("无效的会话文件头")
			}
			if hdr.ID != id {
				return nil, fmt.Errorf("会话 ID 与文件名不一致: file=%s header=%s", id, hdr.ID)
			}
			continue
		}
		var m schema.Message
		if err := json.Unmarshal(line, &m); err != nil {
			return nil, fmt.Errorf("第 %d 行消息: %w", lineNum, err)
		}
		cp := m
		msgs = append(msgs, &cp)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if lineNum == 0 {
		return nil, errors.New("空会话文件")
	}

	sess := &Session{
		store:     s,
		id:        hdr.ID,
		path:      path,
		createdAt: hdr.CreatedAt,
		msgs:      msgs,
	}
	s.mu.Lock()
	s.cache[id] = sess
	s.mu.Unlock()
	return sess, nil
}

// ID 会话标识。
func (sess *Session) ID() string {
	return sess.id
}

// CreatedAt 创建时间（来自文件头）。
func (sess *Session) CreatedAt() time.Time {
	return sess.createdAt
}

// Len 当前消息条数（不含会话头）。
func (sess *Session) Len() int {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return len(sess.msgs)
}

// GetMessages 返回当前消息的浅拷贝切片（元素指针与内部一致，勿修改 *Message）。
func (sess *Session) GetMessages() []*schema.Message {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	out := make([]*schema.Message, len(sess.msgs))
	copy(out, sess.msgs)
	return out
}

// Append 追加一条消息并持久化到 JSONL 行尾。
func (sess *Session) Append(m *schema.Message) error {
	if m == nil {
		return errors.New("nil message")
	}
	line, err := json.Marshal(m)
	if err != nil {
		return err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()

	f, err := os.OpenFile(sess.path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, werr := f.Write(append(line, '\n')); werr != nil {
		_ = f.Close()
		return werr
	}
	if err := f.Close(); err != nil {
		return err
	}
	cp := *m
	sess.msgs = append(sess.msgs, &cp)
	return nil
}

// TruncateLast 从内存与文件中移除末尾 n 条消息（用于调用失败时回滚本轮用户输入等）。
func (sess *Session) TruncateLast(n int) error {
	if n <= 0 {
		return nil
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if len(sess.msgs) < n {
		return fmt.Errorf("无法回滚 %d 条：当前仅 %d 条消息", n, len(sess.msgs))
	}
	sess.msgs = sess.msgs[:len(sess.msgs)-n]
	return sess.rewriteLocked()
}

func (sess *Session) rewriteLocked() error {
	tmp := sess.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	hdr := sessionHeader{Type: "session", ID: sess.id, CreatedAt: sess.createdAt}
	b, err := json.Marshal(hdr)
	if err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	for _, m := range sess.msgs {
		line, err := json.Marshal(m)
		if err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
			return err
		}
		if _, err := w.Write(append(line, '\n')); err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
			return err
		}
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, sess.path); err != nil {
		// Windows 上目标已存在时 Rename 可能失败，先删再改
		_ = os.Remove(sess.path)
		if rerr := os.Rename(tmp, sess.path); rerr != nil {
			return rerr
		}
	}
	return nil
}

// Title 用首条用户消息内容作为标题（截断），无可用时返回默认。
func (sess *Session) Title() string {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	for _, m := range sess.msgs {
		if m != nil && m.Role == schema.User && strings.TrimSpace(m.Content) != "" {
			t := strings.TrimSpace(m.Content)
			if len(t) > 48 {
				return t[:48] + "…"
			}
			return t
		}
	}
	return "New Session"
}
