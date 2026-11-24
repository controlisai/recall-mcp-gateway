package detect

import "fmt"

// ContentDetector scans tool call arguments for risky content.
type ContentDetector struct {
	patterns []ContentPattern
}

// NewContentDetector creates a detector with built-in patterns.
func NewContentDetector() *ContentDetector {
	return &ContentDetector{patterns: BuiltInContentPatterns}
}

// Scan checks all argument values against content patterns.
// Returns signals for any matches found.
func (d *ContentDetector) Scan(args map[string]any) []Signal {
	var signals []Signal
	for key, val := range args {
		str := stringify(val)
		if str == "" {
			continue
		}
		for _, p := range d.patterns {
			if p.Pattern.MatchString(str) {
				signals = append(signals, Signal{
					Name:     p.Name,
					Category: p.Category,
					Severity: p.Severity,
					Message:  fmt.Sprintf("%s detected in argument %q", p.Name, key),
					Detail:   truncate(p.Pattern.FindString(str), 50),
				})
			}
		}
	}
	return signals
}

func stringify(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case fmt.Stringer:
		return val.String()
	case map[string]any:
		// Recursively stringify map values
		var result string
		for _, mv := range val {
			result += stringify(mv) + " "
		}
		return result
	case []any:
		var result string
		for _, item := range val {
			result += stringify(item) + " "
		}
		return result
	default:
		return fmt.Sprintf("%v", v)
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
