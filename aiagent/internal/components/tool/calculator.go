// Package tool 实现 InvokableTool，供 ADK / ToolsNode 注册；建议用 utils.InferTool 从结构体推断参数。
package tool

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	tutils "github.com/cloudwego/eino/components/tool/utils"
)

// calculatorInput 由模型以 JSON 参数调用（字段名会出现在 tool schema 中）。
type calculatorInput struct {
	A float64 `json:"a" jsonschema:"description=第一个加数"`
	B float64 `json:"b" jsonschema:"description=第二个加数"`
}

// NewCalculatorTool 提供基础加法，演示无外部 IO 的工具；工具名在模型侧为 "calculator"。
func NewCalculatorTool() (tool.InvokableTool, error) {
	return tutils.InferTool("calculator", "将两个数相加。参数为两个浮点数 a、b。", func(_ context.Context, in calculatorInput) (string, error) {
		return fmt.Sprintf("结果：%.6g + %.6g = %.6g", in.A, in.B, in.A+in.B), nil
	})
}
