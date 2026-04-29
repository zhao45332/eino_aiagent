// Package tool 提供可调用的工具；本文件为唯一计算工具 calculator（Streamable），多端加减与流式分段输出合一。
package tool

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	tutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

// calculatorInput 由模型以 JSON 传入；从左到右逐项累加，减法把该项写成负数。
type calculatorInput struct {
	Numbers []float64 `json:"numbers" jsonschema:"description=与算式逐项对应，从左到右。例：1+2+...+6 写作 [1,2,3,4,5,6]；减法用负数。两数加法 [a,b]；至少一项"`
}

// NewCalculatorTool 构造 [tool.StreamableTool]：
// utils.InferStreamTool 会生成实现接口的工具（运行时走 StreamableRun，而非 Invokable 的 Invoke）。
func NewCalculatorTool() (tool.StreamableTool, error) {
	desc := `精确加减的唯一入口：对 numbers 逐项累加（负数表示减）。必须使用本工具并得到流式输出的「最终结果」后再答复用户；禁止口算顶替。示例：题目 1+2+...+6 → numbers=[1,2,3,4,5,6]。`
	st, err := tutils.InferStreamTool("calculator", desc, func(_ context.Context, in calculatorInput) (*schema.StreamReader[string], error) {
		if len(in.Numbers) == 0 {
			return nil, fmt.Errorf("numbers 不能为空")
		}
		if len(in.Numbers) == 1 {
			v := in.Numbers[0]
			return schema.StreamReaderFromArray([]string{
				fmt.Sprintf("结果：%.6g\n", v),
			}), nil
		}
		chunks := make([]string, 0, len(in.Numbers))
		var acc float64
		for i, n := range in.Numbers {
			acc += n
			chunks = append(chunks, fmt.Sprintf("加到第 %d 项后记为：%.6g\n", i+1, acc))
		}
		chunks = append(chunks, fmt.Sprintf("最终结果：%.6g", acc))
		return schema.StreamReaderFromArray(chunks), nil
	})
	if err != nil {
		return nil, err
	}
	var _ tool.StreamableTool = st
	return st, nil
}
