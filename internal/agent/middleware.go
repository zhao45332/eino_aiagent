package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

type safeToolMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

func newSafeToolMiddleware() adk.ChatModelAgentMiddleware {
	return &safeToolMiddleware{BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{}}
}

func (m *safeToolMiddleware) WrapInvokableToolCall(_ context.Context, endpoint adk.InvokableToolCallEndpoint, _ *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, args string, opts ...tool.Option) (string, error) {
		result, err := endpoint(ctx, args, opts...)
		if err != nil {
			if _, ok := compose.IsInterruptRerunError(err); ok {
				return "", err
			}
			return formatToolError(err), nil
		}
		return result, nil
	}, nil
}

func (m *safeToolMiddleware) WrapStreamableToolCall(_ context.Context, endpoint adk.StreamableToolCallEndpoint, _ *adk.ToolContext) (adk.StreamableToolCallEndpoint, error) {
	return func(ctx context.Context, args string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		result, err := endpoint(ctx, args, opts...)
		if err != nil {
			if _, ok := compose.IsInterruptRerunError(err); ok {
				return nil, err
			}
			return schema.StreamReaderFromArray([]string{formatToolError(err)}), nil
		}
		return result, nil
	}, nil
}

func formatToolError(err error) string {
	return fmt.Sprintf("[tool error] %v", err)
}

func newAgentHandlers() []adk.ChatModelAgentMiddleware {
	return []adk.ChatModelAgentMiddleware{newSafeToolMiddleware()}
}

func newModelRetryConfig() *adk.ModelRetryConfig {
	return &adk.ModelRetryConfig{
		MaxRetries:  3,
		IsRetryAble: isTemporaryModelError,
	}
}

func isTemporaryModelError(_ context.Context, err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	retryableSubstrings := []string{
		"429",
		"too many requests",
		"rate limit",
		"qpm",
		"temporarily unavailable",
		"timeout",
		"connection reset",
		"server overloaded",
	}
	for _, s := range retryableSubstrings {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}
