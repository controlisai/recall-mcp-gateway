package rules

import (
	"testing"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/config"
)

func TestCheckSchedule_Nil(t *testing.T) {
	if !CheckSchedule(nil) {
		t.Error("nil schedule should return true")
	}
}

func TestMatchCron_Always(t *testing.T) {
	if !matchCron("* * * * *", time.Now()) {
		t.Error("'* * * * *' should always match")
	}
}

func TestMatchCron_SpecificHour(t *testing.T) {
	at10am := time.Date(2026, 8, 22, 10, 30, 0, 0, time.UTC)
	at3am := time.Date(2026, 8, 22, 3, 30, 0, 0, time.UTC)

	// 9am-5pm
	if !matchCron("* 9-17 * * *", at10am) {
		t.Error("10am should match 9-17")
	}
	if matchCron("* 9-17 * * *", at3am) {
		t.Error("3am should not match 9-17")
	}
}

func TestMatchCron_Weekdays(t *testing.T) {
	// 2026-08-22 is a Saturday (day 6)
	saturday := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	// 2026-08-24 is a Monday (day 1)
	monday := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)

	// Weekdays only (1-5)
	if matchCron("* * * * 1-5", saturday) {
		t.Error("Saturday should not match 1-5")
	}
	if !matchCron("* * * * 1-5", monday) {
		t.Error("Monday should match 1-5")
	}
}

func TestMatchField_List(t *testing.T) {
	if !matchField("1,3,5", 3) {
		t.Error("3 should match 1,3,5")
	}
	if matchField("1,3,5", 2) {
		t.Error("2 should not match 1,3,5")
	}
}

func TestCheckSchedule_Inside(t *testing.T) {
	// Test that Inside=true means rule applies during the window
	schedule := &config.ScheduleConfig{
		Cron:   "* * * * *", // always active
		Inside: true,
	}
	if !CheckSchedule(schedule) {
		t.Error("Inside=true with always-active cron should return true")
	}
}

func TestCheckSchedule_Outside(t *testing.T) {
	schedule := &config.ScheduleConfig{
		Cron:   "* * * * *", // always active
		Inside: false,
	}
	if CheckSchedule(schedule) {
		t.Error("Inside=false with always-active cron should return false")
	}
}
