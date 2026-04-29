// Package model 提供 OpenAI 兼容 ChatModel（eino-ext openai），由 config + bootstrap 选用。
package model

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
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
	t.IdleConnTimeout = 90 * time.Second
	t.ResponseHeaderTimeout = 60 * time.Second
	t.ExpectContinueTimeout = 1 * time.Second

	disableH2 := strings.Contains(strings.ToLower(baseURL), "bigmodel.cn") ||
		strings.TrimSpace(os.Getenv("OPENAI_HTTP_DISABLE_HTTP2")) == "1"
	if disableH2 {
		t.ForceAttemptHTTP2 = false
		t.TLSNextProto = map[string]func(authority string, c *tls.Conn) http.RoundTripper{}
		t.TLSClientConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
			NextProtos: []string{"http/1.1"},
		}
	}
	if strings.TrimSpace(os.Getenv("OPENAI_HTTP_DISABLE_KEEPALIVE")) == "1" {
		t.DisableKeepAlives = true
	}

	return &http.Client{
		Timeout:   requestTimeout,
		Transport: t,
	}
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
