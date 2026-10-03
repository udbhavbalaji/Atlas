package routing

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

var explicitClockPattern = regexp.MustCompile(`(?i)\b(1[0-2]|0?[1-9])(?:[:.](\d{2}))?\s*([ap])\.?m\.?`)

// Models sometimes place a standalone reminder time in the task-only due_at
// slot. Normalize by the selected action before checking the user's clock.
func NormalizePlanTiming(plan ActionPlan, state State) ActionPlan {
	if plan.Action == "reminder" && plan.ReminderAt == "" && plan.DueAt != "" {
		plan.ReminderAt, plan.DueAt = plan.DueAt, ""
	}
	return normalizeExplicitClock(plan, state)
}

func explicitClock(text string) (int, int, bool) {
	matches := explicitClockPattern.FindAllStringSubmatch(text, -1)
	if len(matches) != 1 {
		return 0, 0, false
	}
	hour, _ := strconv.Atoi(matches[0][1])
	minute := 0
	if matches[0][2] != "" {
		minute, _ = strconv.Atoi(matches[0][2])
	}
	if minute > 59 {
		return 0, 0, false
	}
	if hour == 12 {
		hour = 0
	}
	if strings.EqualFold(matches[0][3], "p") {
		hour += 12
	}
	return hour, minute, true
}

// A model can emit 08:00Z for a user's 8 AM in India. If exactly one clock
// time is stated, align the extracted instant with that local clock time.
// An earlier linked reminder keeps its offset from the task deadline.
func normalizeExplicitClock(plan ActionPlan, state State) ActionPlan {
	hour, minute, ok := explicitClock(state.Text)
	if !ok {
		return plan
	}
	zone, err := time.LoadLocation(state.Timezone)
	if err != nil {
		return plan
	}
	adjust := func(value string) (string, time.Duration) {
		instant, parseErr := time.Parse(time.RFC3339Nano, value)
		if parseErr != nil {
			return value, 0
		}
		local := instant.In(zone)
		if local.Hour() == hour && local.Minute() == minute {
			return value, 0
		}
		date, hasRelativeDate := relativeDate(state)
		if !hasRelativeDate {
			// Without a stated relative day, only reinterpret a timestamp
			// whose own clock digits match the user's requested time.
			if instant.Hour() != hour || instant.Minute() != minute {
				return value, 0
			}
			date = instant
		}
		y, month, day := date.Date()
		corrected := time.Date(y, month, day, hour, minute, 0, 0, zone)
		return corrected.Format(time.RFC3339), corrected.Sub(instant)
	}
	switch plan.Action {
	case "reminder":
		if plan.ReminderAt != "" {
			plan.ReminderAt, _ = adjust(plan.ReminderAt)
		}
	case "task":
		if plan.DueAt != "" {
			var shift time.Duration
			plan.DueAt, shift = adjust(plan.DueAt)
			if shift != 0 && plan.ReminderAt != "" {
				if earlier, parseErr := time.Parse(time.RFC3339Nano, plan.ReminderAt); parseErr == nil {
					plan.ReminderAt = earlier.Add(shift).Format(time.RFC3339)
				}
			}
		} else if plan.ReminderAt != "" {
			plan.ReminderAt, _ = adjust(plan.ReminderAt)
		}
	case "edit":
		if (plan.Field == "scheduled_at" || plan.Field == "due_at") && plan.Value != "" {
			plan.Value, _ = adjust(plan.Value)
		}
	}
	return plan
}
