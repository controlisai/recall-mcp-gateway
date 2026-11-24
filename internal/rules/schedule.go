package rules

import (
	"strconv"
	"strings"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/config"
)

// CheckSchedule returns true if the current time is within the schedule window.
// If schedule is nil, returns true (no schedule restriction).
// When Inside=true, the rule only applies during the scheduled window.
// When Inside=false, the rule only applies outside the scheduled window.
func CheckSchedule(schedule *config.ScheduleConfig) bool {
	if schedule == nil {
		return true
	}

	now := time.Now()
	if schedule.Timezone != "" {
		if loc, err := time.LoadLocation(schedule.Timezone); err == nil {
			now = now.In(loc)
		}
	}

	active := matchCron(schedule.Cron, now)

	if schedule.Inside {
		return active // rule applies when inside the window
	}
	return !active // rule applies when outside the window
}

// matchCron matches a simplified cron expression against a time.
// Format: "minute hour day-of-month month day-of-week"
// Supports: *, specific numbers, ranges (1-5), lists (1,3,5).
// Examples:
//   - "* 9-17 * * 1-5"  → weekdays 9am-5pm
//   - "* * * * *"        → always
//   - "0 0 * * 0"        → midnight on Sundays
func matchCron(expr string, t time.Time) bool {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return true // invalid cron = always match (fail-open)
	}

	minute := t.Minute()
	hour := t.Hour()
	dom := t.Day()
	month := int(t.Month())
	dow := int(t.Weekday()) // 0=Sunday

	return matchField(fields[0], minute) &&
		matchField(fields[1], hour) &&
		matchField(fields[2], dom) &&
		matchField(fields[3], month) &&
		matchField(fields[4], dow)
}

func matchField(field string, value int) bool {
	if field == "*" {
		return true
	}

	// Handle lists: "1,3,5"
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)

		// Handle ranges: "1-5"
		if strings.Contains(part, "-") {
			rangeParts := strings.SplitN(part, "-", 2)
			low, err1 := strconv.Atoi(rangeParts[0])
			high, err2 := strconv.Atoi(rangeParts[1])
			if err1 == nil && err2 == nil && value >= low && value <= high {
				return true
			}
			continue
		}

		// Handle exact value
		if num, err := strconv.Atoi(part); err == nil && num == value {
			return true
		}
	}

	return false
}
