package config

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseScheduleTime parses a "HH:MM" schedule time (local time, as used by
// GenAI summary schedules). Hours 0-23, minutes 0-59.
func ParseScheduleTime(s string) (hour, min int, err error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("time %q must be HH:MM", s)
	}
	hour, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("time %q must be HH:MM", s)
	}
	min, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("time %q must be HH:MM", s)
	}
	if hour < 0 || hour > 23 || min < 0 || min > 59 {
		return 0, 0, fmt.Errorf("time %q is out of range (00:00-23:59)", s)
	}
	return hour, min, nil
}

// ParseScheduleWindow parses a GenAI summary window: "today" (midnight to
// run time) or "last_Nh" (N hours back from run time, 1-168). An empty
// window defaults to "today". It returns (isToday, hours, err).
func ParseScheduleWindow(s string) (bool, int, error) {
	if s == "" || s == "today" {
		return true, 0, nil
	}
	if strings.HasPrefix(s, "last_") && strings.HasSuffix(s, "h") {
		if n, err := strconv.Atoi(s[len("last_") : len(s)-1]); err == nil && n >= 1 && n <= 168 {
			return false, n, nil
		}
	}
	return false, 0, fmt.Errorf("window %q must be 'today' or 'last_Nh' (N hours, 1-168)", s)
}
