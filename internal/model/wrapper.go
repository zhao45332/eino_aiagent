// Package model 提供 OpenAI 兼容 ChatModel（eino-ext openai），由 config + bootstrap 选用。
package model

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cloudwego/eino-ext/components/model/openai"
	emodel "github.com/cloudwego/eino/components/model"

	"aiagent/internal/config"
)

// NewOpenAICompatHTTPClient 为 OpenAI 兼容 API（含智谱 bigmodel.cn）构造 http.Client。
// 智谱等环境下若仍协商 HTTP/2，net/http 会按 HTTP/1.x 读响应，易出现 malformed HTTP response（实为 HTTP/2 二进制帧）。
// 因此对该类域名强制 ALPN 仅 http/1.1，并关闭 Transport 的 HTTP/2 尝试。
func NewOpenAICompatHTTPClient(baseURL string, requestTimeout time.Duration) *http.Client {
	if requestTimeout <= 0 {
		requestTimeout = 180 * time.Second
	}

	tr, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Client{Timeout: requestTimeout}
	}
	t := tr.Clone()
	t.MaxIdleConns = 100
	t.MaxIdleConnsPerHost = 32
	t.IdleConnTimeout = 120 * time.Second
	t.ResponseHeaderTimeout = 120 * time.Second
	t.ExpectContinueTimeout = 1 * time.Second

	isBigModel := strings.Contains(strings.ToLower(baseURL), "bigmodel.cn")

	disableH2 := isBigModel || strings.TrimSpace(os.Getenv("OPENAI_HTTP_DISABLE_HTTP2")) == "1"
	if disableH2 {
		t.ForceAttemptHTTP2 = false
		t.TLSNextProto = map[string]func(authority string, c *tls.Conn) http.RoundTripper{}
		t.TLSClientConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
			NextProtos: []string{"http/1.1"},
		}
	}
	switch strings.TrimSpace(os.Getenv("OPENAI_HTTP_DISABLE_KEEPALIVE")) {
	case "1":
		t.DisableKeepAlives = true
	case "0":
		t.DisableKeepAlives = false
	default:
		// 智谱侧偶发 EOF 多与复用空闲长连接有关；默认禁 keep-alive，可用 =0 恢复
		if isBigModel {
			t.DisableKeepAlives = true
		}
	}

	transport := http.RoundTripper(t)
	if n := eofRetryCount(baseURL); n > 0 {
		transport = newEOFRetryRoundTripper(transport, n, eofRetryBackoff())
	}

	return &http.Client{
		Timeout:   requestTimeout,
		Transport: transport,
	}
}

func eofRetryCount(baseURL string) int {
	if s := strings.TrimSpace(os.Getenv("OPENAI_HTTP_EOF_RETRIES")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			return n
		}
	}
	// max 表示「除首次请求外额外重试次数」：=2 表示最多共 3 次 HTTP。
	if strings.Contains(strings.ToLower(baseURL), "bigmodel.cn") {
		return 2
	}
	return 0
}

func eofRetryBackoff() time.Duration {
	if s := strings.TrimSpace(os.Getenv("OPENAI_HTTP_EOF_RETRY_MS")); s != "" {
		if ms, err := strconv.Atoi(s); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return 400 * time.Millisecond
}

// eofRetryRoundTripper 在 Post 对话等请求上出现瞬时连接错误（EOF、reset）时重试若干次，缓解智谱网关偶发现象。
type eofRetryRoundTripper struct {
	next http.RoundTripper
	max  int
	base time.Duration
}

func newEOFRetryRoundTripper(next http.RoundTripper, max int, base time.Duration) *eofRetryRoundTripper {
	if max < 0 {
		max = 0
	}
	return &eofRetryRoundTripper{next: next, max: max, base: base}
}

func (r *eofRetryRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	orig := req
	ctx := orig.Context()
	var lastErr error
	for attempt := 0; attempt <= r.max; attempt++ {
		if attempt > 0 {
			d := r.base * time.Duration(1<<minInt(attempt-1, 5))
			if d > 10*time.Second {
				d = 10 * time.Second
			}
			select {
			case <-time.After(d):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			if orig.GetBody == nil {
				return nil, lastErr
			}
			body, err := orig.GetBody()
			if err != nil {
				return nil, fmt.Errorf("%w（重试读 body: %v）", lastErr, err)
			}
			cloned := orig.Clone(ctx)
			cloned.Body = io.NopCloser(body)
			resp, err := r.next.RoundTrip(cloned)
			if err == nil {
				return resp, nil
			}
			lastErr = err
			if attempt == r.max || !isRetriableLLMTransportErr(err) {
				return nil, err
			}
			continue
		}

		resp, err := r.next.RoundTrip(orig)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if attempt == r.max || !isRetriableLLMTransportErr(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func isRetriableLLMTransportErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	var op *net.OpError
	if errors.As(err, &op) {
		if sys, ok := op.Err.(syscall.Errno); ok {
			switch sys {
			case syscall.ECONNRESET, syscall.ECONNABORTED, syscall.EPIPE:
				return true
			}
		}
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "eof") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "broken pipe") ||
		strings.Contains(s, "tls") && strings.Contains(s, "timeout")
}

// NewOpenAI 创建 OpenAI 兼容 ChatModel。
func NewOpenAI(ctx context.Context, c *config.Config) (emodel.BaseChatModel, error) {
	timeout := 180 * time.Second
	if s := strings.TrimSpace(os.Getenv("OPENAI_HTTP_TIMEOUT")); s != "" {
		if sec, err := strconv.Atoi(s); err == nil && sec > 0 {
			timeout = time.Duration(sec) * time.Second
		}
	}
	cfg := &openai.ChatModelConfig{
		APIKey:     c.APIKey,
		BaseURL:    c.BaseURL,
		Model:      c.Model,
		HTTPClient: NewOpenAICompatHTTPClient(c.BaseURL, timeout),
	}
	cm, err := openai.NewChatModel(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("openai chat model: %w", err)
	}
	return cm, nil
}
