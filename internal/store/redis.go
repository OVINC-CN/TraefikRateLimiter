package store

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OVINC-CN/TraefikRateLimiter/internal/config"
	"github.com/OVINC-CN/TraefikRateLimiter/internal/constant"
)

type RedisStore struct {
	cfg *config.RedisConfig

	mu *sync.Mutex

	conn net.Conn
	rd   *bufio.Reader

	incrScriptSHA string
}

func (s *RedisStore) connect() error {
	var err error

	// lock
	s.mu.Lock()
	defer s.mu.Unlock()

	// reset existing connection if any
	s.resetConn()

	// connect
	if s.conn, err = net.DialTimeout("tcp", s.cfg.Addr, s.cfg.TimeoutInner); err != nil {
		return fmt.Errorf("redis: dial %s: %w", s.cfg.Addr, err)
	}
	s.rd = bufio.NewReader(s.conn)

	// auth and select db
	if s.cfg.Password != "" {
		if err = s.send("AUTH", s.cfg.Password); err != nil {
			s.resetConn()
			return fmt.Errorf("redis: AUTH: %w", err)
		}
		if _, err = s.recv(); err != nil {
			s.resetConn()
			return fmt.Errorf("redis: AUTH response: %w", err)
		}
	}

	// select db if not zero
	if s.cfg.DB != 0 {
		if err = s.send("SELECT", strconv.Itoa(int(s.cfg.DB))); err != nil {
			s.resetConn()
			return fmt.Errorf("redis: SELECT: %w", err)
		}
		if _, err := s.recv(); err != nil {
			s.resetConn()
			return fmt.Errorf("redis: SELECT response: %w", err)
		}
	}

	// load Lua script and cache its SHA1
	if err = s.loadIncrScript(); err != nil {
		s.resetConn()
		return fmt.Errorf("redis: SCRIPT LOAD: %w", err)
	}

	return nil
}

func (s *RedisStore) resetConn() {
	if s.conn != nil {
		_ = s.conn.Close()
	}
	s.conn = nil
	s.rd = nil
	s.incrScriptSHA = ""
}

func (s *RedisStore) send(args ...string) error {
	// build RESP inline array
	var b strings.Builder
	if _, err := fmt.Fprintf(&b, "*%d\r\n", len(args)); err != nil {
		return err
	}
	// RESP bulk string for each argument
	for _, a := range args {
		if _, err := fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(a), a); err != nil {
			return err
		}
	}
	// set write deadline and send
	if err := s.conn.SetWriteDeadline(time.Now().Add(s.cfg.TimeoutInner)); err != nil {
		return err
	}
	// write the command
	_, err := io.WriteString(s.conn, b.String())
	return err
}

func (s *RedisStore) recv() (interface{}, error) {
	if err := s.conn.SetReadDeadline(time.Now().Add(s.cfg.TimeoutInner)); err != nil {
		return nil, err
	}
	return s.readOne()
}

func (s *RedisStore) readOne() (interface{}, error) {
	// read the first
	line, err := s.rd.ReadString('\n')
	if err != nil {
		return nil, err
	}
	// trim trailing \r\n
	line = strings.TrimRight(line, "\r\n")
	if len(line) == 0 {
		return nil, fmt.Errorf("redis: empty response line")
	}
	// parse based on the first byte
	switch line[0] {
	case '+':
		return line[1:], nil
	case '-':
		return nil, fmt.Errorf("redis: server error: %s", line[1:])
	case ':':
		n, err := strconv.ParseInt(line[1:], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("redis: malformed integer %q", line)
		}
		return n, nil
	case '$':
		sz, err := strconv.Atoi(line[1:])
		if err != nil {
			return nil, fmt.Errorf("redis: malformed bulk-string size %q", line)
		}
		if sz < 0 {
			return nil, nil // null bulk string
		}
		buf := make([]byte, sz+2) // +2 for trailing \r\n
		if _, err := io.ReadFull(s.rd, buf); err != nil {
			return nil, fmt.Errorf("redis: read bulk-string: %w", err)
		}
		return string(buf[:sz]), nil
	case '*':
		n, err := strconv.Atoi(line[1:])
		if err != nil {
			return nil, fmt.Errorf("redis: malformed array length %q", line)
		}
		if n < 0 {
			return nil, nil // null array
		}
		arr := make([]interface{}, n)
		for i := range arr {
			v, err := s.readOne()
			if err != nil {
				return nil, err
			}
			arr[i] = v
		}
		return arr, nil
	default:
		return nil, fmt.Errorf("redis: unknown type byte %q", line[0])
	}
}

func (s *RedisStore) loadIncrScript() error {
	if err := s.send("SCRIPT", "LOAD", constant.IncrTTLScript); err != nil {
		return err
	}
	resp, err := s.recv()
	if err != nil {
		return err
	}
	sha, ok := resp.(string)
	if !ok || sha == "" {
		return fmt.Errorf("redis: malformed SCRIPT LOAD result %v", resp)
	}
	s.incrScriptSHA = sha
	return nil
}

func (s *RedisStore) evalIncr(key string, ttlSec int64) (int64, int64, error) {
	// call lua script
	ttlStr := strconv.FormatInt(ttlSec, 10)
	if err := s.send("EVALSHA", s.incrScriptSHA, "1", key, ttlStr); err != nil {
		return 0, 0, err
	}
	// receive response
	resp, err := s.recv()
	if err != nil {
		return 0, 0, err
	}
	// parse response as array of [count, remainSec]
	arr, ok := resp.([]interface{})
	if !ok || len(arr) < 2 {
		err = fmt.Errorf("redis: unexpected EVALSHA result %v", resp)
		return 0, 0, err
	}
	count, ok := arr[0].(int64)
	if !ok {
		err = fmt.Errorf("redis: unexpected count type %T", arr[0])
		return 0, 0, err
	}
	remainSec, ok := arr[1].(int64)
	if !ok {
		err = fmt.Errorf("redis: unexpected ttl type %T", arr[1])
		return 0, 0, err
	}
	if remainSec < 0 {
		remainSec = ttlSec
	}
	return count, remainSec, nil
}

func (s *RedisStore) Incr(key string, ttl time.Duration, now time.Time) (int64, *time.Time, error) {
	// init
	if s.cfg.KeyPrefix != "" {
		key = s.cfg.KeyPrefix + key
	}
	ttlSec := int64(ttl / time.Second)
	if ttlSec < 1 {
		ttlSec = 1
	}

	// lock
	s.mu.Lock()
	defer s.mu.Unlock()

	// increment in Redis
	count, remainSec, err := s.evalIncr(key, ttlSec)
	if err != nil {
		return 0, nil, err
	}

	// calculate absolute expiration time
	expireAt := now.Add(time.Duration(remainSec) * time.Second)
	return count, &expireAt, nil
}

func (s *RedisStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resetConn()
}

func NewRedisStore(cfg *config.RedisConfig) (*RedisStore, error) {
	s := &RedisStore{cfg: cfg, mu: &sync.Mutex{}}
	if err := s.connect(); err != nil {
		return nil, err
	}
	return s, nil
}
