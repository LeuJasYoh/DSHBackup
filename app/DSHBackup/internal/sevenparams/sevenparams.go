package sevenparams

import (
	"fmt"
	"regexp"
	"strings"
)

var allowedPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^-md=(16|32|64|128|256|512|1024)m$`),
	regexp.MustCompile(`^-mmt=(?:[1-9]|[1-5][0-9]|6[0-4])$`),
	regexp.MustCompile(`^-mfb=(?:[5-9]|[1-9][0-9]|1[0-9]{2}|2[0-6][0-9]|27[0-3])$`),
	regexp.MustCompile(`^-ms=(?:on|off)$`),
	regexp.MustCompile(`^-mqs=(?:on|off)$`),
}

func ValidateExtra(args []string) ([]string, error) {
	if len(args) > 12 {
		return nil, fmt.Errorf("高级参数过多（最多 12 个）")
	}
	result := make([]string, 0, len(args))
	for _, raw := range args {
		arg := strings.TrimSpace(raw)
		if arg == "" {
			continue
		}
		if len(arg) > 64 {
			return nil, fmt.Errorf("高级参数过长：%s", arg)
		}
		ok := false
		for _, pattern := range allowedPatterns {
			if pattern.MatchString(arg) {
				ok = true
				break
			}
		}
		if !ok {
			return nil, fmt.Errorf("不允许的 7z 高级参数：%s", arg)
		}
		result = append(result, arg)
	}
	return result, nil
}

func LevelMX(level string) int {
	switch level {
	case "store":
		return 0
	case "fast":
		return 1
	case "quick":
		return 3
	case "high":
		return 7
	case "max":
		return 9
	default:
		return 5
	}
}

func LevelForMX(value int) string {
	switch {
	case value <= 0:
		return "store"
	case value == 1:
		return "fast"
	case value <= 3:
		return "quick"
	case value <= 6:
		return "balanced"
	case value <= 8:
		return "high"
	default:
		return "max"
	}
}

func DictionaryAllowed(value int) bool {
	switch value {
	case 0, 16, 32, 64, 128, 256, 512, 1024:
		return true
	default:
		return false
	}
}

func ThreadAllowed(value int) bool {
	return value == 0 || (value >= 1 && value <= 64)
}

func BuildArgs(level string, dictionaryMB, threads int, extra []string) []string {
	args := []string{fmt.Sprintf("-mx=%d", LevelMX(level))}
	if dictionaryMB > 0 {
		args = append(args, fmt.Sprintf("-md=%dm", dictionaryMB))
	}
	if threads > 0 {
		args = append(args, fmt.Sprintf("-mmt=%d", threads))
	}
	args = append(args, extra...)
	return args
}
