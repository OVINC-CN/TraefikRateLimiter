package TraefikRateLimiter

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// incrTTLScript atomically increments a key and sets its TTL on first use.
// Returns a two-element array [count, remaining_ttl_seconds].
const incrTTLScript = `local c=redis.call('INCR',KEYS[1]) ` +
	`if c==1 then redis.call('EXPIRE',KEYS[1],ARGV[1]) end ` +
	`return {c,redis.call('TTL',KEYS[1])}`

// redisStore is a rate-limit counter backend backed by Redis.
// A single TCP connection is kept open and protected by a mutex.
// On Redis errors, the current request fails and the error is returned.
type redisStore struct {
	mu       sync.Mutex
	addr     string
	password string
	db       int
	prefix   string

	conn net.Conn
	rd   *bufio.Reader
}

func newRedisStore(cfg RedisConfig) (*redisStore, error) {
	addr := cfg.Addr
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	s := &redisStore{
		addr:     addr,
		password: cfg.Password,
		db:       cfg.DB,
		prefix:   cfg.KeyPrefix,
	}
	if err := s.connect(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *redisStore) resetConn() {
	if s.conn != nil {
		_ = s.conn.Close()
	}
	s.conn = nil
	s.rd = nil
}

// connect establishes (or re-establishes) the TCP connection, runs AUTH and
// SELECT when configured, and resets the read buffer.
func (s *redisStore) connect() error {
	s.resetConn()
	conn, err := net.DialTimeout("tcp", s.addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("redis: dial %s: %w", s.addr, err)
	}
	s.conn = conn
	s.rd = bufio.NewReader(conn)

	if s.password != "" {
		if err := s.send("AUTH", s.password); err != nil {
			s.resetConn()
			return fmt.Errorf("redis: AUTH: %w", err)
		}
		if _, err := s.recv(); err != nil {
			s.resetConn()
			return fmt.Errorf("redis: AUTH response: %w", err)
		}
	}

	if s.db != 0 {
		if err := s.send("SELECT", strconv.Itoa(s.db)); err != nil {
			s.resetConn()
			return fmt.Errorf("redis: SELECT: %w", err)
		}
		if _, err := s.recv(); err != nil {
			s.resetConn()
			return fmt.Errorf("redis: SELECT response: %w", err)
		}
	}

	return nil
}

// send serialises args as a RESP inline array and writes it to the connection.
func (s *redisStore) send(args ...string) error {
	if s.conn == nil {
		return net.ErrClosed
	}
	var b strings.Builder
	fmt.Fprintf(&b, "*%d\r\n", len(args))
	for _, a := range args {
		fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(a), a)
	}
	_ = s.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	_, err := io.WriteString(s.conn, b.String())
	return err
}

// recv reads one RESP value from the connection.
func (s *redisStore) recv() (interface{}, error) {
	if s.conn == nil || s.rd == nil {
		return nil, net.ErrClosed
	}
	_ = s.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	return s.readOne()
}

// readOne recursively reads a single RESP value.
func (s *redisStore) readOne() (interface{}, error) {
	line, err := s.rd.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) == 0 {
		return nil, fmt.Errorf("redis: empty response line")
	}
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

// evalIncr runs incrTTLScript via EVAL and returns (count, remaining_ttl).
func (s *redisStore) evalIncr(key string, ttlSec int64) (count int64, remainSec int64, err error) {
	ttlStr := strconv.FormatInt(ttlSec, 10)
	if err = s.send("EVAL", incrTTLScript, "1", key, ttlStr); err != nil {
		return
	}
	resp, err := s.recv()
	if err != nil {
		return
	}
	arr, ok := resp.([]interface{})
	if !ok || len(arr) < 2 {
		err = fmt.Errorf("redis: unexpected EVAL result %v", resp)
		return
	}
	count, ok = arr[0].(int64)
	if !ok {
		err = fmt.Errorf("redis: unexpected count type %T", arr[0])
		return
	}
	remainSec, ok = arr[1].(int64)
	if !ok {
		err = fmt.Errorf("redis: unexpected ttl type %T", arr[1])
		return
	}
	if remainSec < 0 {
		remainSec = ttlSec // fallback when TTL is not set
	}
	return
}

// Incr increments the counter for key in Redis and returns the current count
// and absolute expiration time.
func (s *redisStore) Incr(key string, ttl time.Duration, now time.Time) (int64, time.Time, error) {
	if s.prefix != "" {
		key = s.prefix + key
	}
	ttlSec := int64(ttl / time.Second)
	if ttlSec < 1 {
		ttlSec = 1
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	count, remainSec, err := s.evalIncr(key, ttlSec)
	if err != nil {
		if isNetworkError(err) {
			// Reconnect for subsequent requests. Current request still fails.
			_ = s.connect()
		}
		return 0, time.Time{}, err
	}

	expireAt := now.Add(time.Duration(remainSec) * time.Second)
	return count, expireAt, nil
}

// Close releases the underlying Redis connection.
func (s *redisStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return nil
	}
	err := s.conn.Close()
	s.conn = nil
	s.rd = nil
	return err
}

func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}
