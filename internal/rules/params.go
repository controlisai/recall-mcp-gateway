package rules

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/controlisai/recall-mcp-gateway/internal/config"
)

// CheckParamBlocks evaluates param_blocks conditions against tool call arguments.
// Returns the first matching block's reason, or empty string if no match.
func CheckParamBlocks(blocks []config.ParamBlockConfig, args map[string]any) (matched bool, reason string) {
	for _, block := range blocks {
		val, ok := args[block.Param]
		if !ok {
			continue
		}

		str := stringify(val)
		if str == "" {
			continue
		}

		re, err := regexp.Compile(block.Pattern)
		if err != nil {
			continue // invalid regex = skip
		}

		if re.MatchString(str) {
			reason := block.Reason
			if reason == "" {
				reason = fmt.Sprintf("parameter %q matched blocked pattern", block.Param)
			}
			return true, reason
		}
	}
	return false, ""
}

func stringify(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case fmt.Stringer:
		return val.String()
	case map[string]any:
		var result string
		for _, mv := range val {
			result += stringify(mv) + " "
		}
		return result
	case []any:
		parts := make([]string, len(val))
		for i, item := range val {
			parts[i] = stringify(item)
		}
		return strings.Join(parts, " ")
	default:
		return fmt.Sprintf("%v", v)
	}
}
