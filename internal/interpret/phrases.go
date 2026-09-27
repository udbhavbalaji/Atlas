package interpret

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// normalizeCommonTime expands complete idioms into the existing strict parser.
// It never removes unrecognized suffixes, so competing times remain unresolved.
func normalizeCommonTime(text string, now time.Time) (string, []string, bool) {
	assumptions := []string{}
	if m := regexp.MustCompile(`^(?:in|after) (?:half an? hour|a half hour|half hour)$`).FindString(text); m != "" {
		return "in 30 minutes", []string{"Half an hour means 30 minutes from the reference time."}, false
	}
	if regexp.MustCompile(`^(?:in|after) (?:a quarter of an? hour|a quarter hour|quarter of an? hour)$`).MatchString(text) {
		return "in 15 minutes", []string{"A quarter hour means 15 minutes from the reference time."}, false
	}
	if m := regexp.MustCompile(`^(?:in|after) (?:a )?couple(?: of)? (seconds?|minutes?|hours?|days?|weeks?)$`).FindStringSubmatch(text); m != nil {
		return "in 2 " + m[1], []string{"A couple means two; review the resulting time."}, false
	}
	if strings.HasPrefix(text, "after ") && durationPattern.MatchString("in "+strings.TrimPrefix(text, "after ")) {
		return "in " + strings.TrimPrefix(text, "after "), []string{"After an interval means that interval from the reference time."}, false
	}
	// Calendar boundaries are inclusive of the current local day/week/month.
	date := now
	clock := ""
	meaning := ""
	switch {
	case regexp.MustCompile(`^(?:(?:the )?end of (?:the )?(?:day|today)|eod|today (?:by )?(?:the )?end of (?:the )?day)$`).MatchString(text):
		clock = "23:59"
		meaning = "End of day means today at 11:59 PM."
	case regexp.MustCompile(`^(?:(?:the )?end of tomorrow|tomorrow (?:by )?(?:the )?end of (?:the )?day)$`).MatchString(text):
		date = now.AddDate(0, 0, 1)
		clock = "23:59"
		meaning = "End of tomorrow means tomorrow at 11:59 PM."
	case regexp.MustCompile(`^(?:(?:the )?end of (?:the |this )?week|eow)$`).MatchString(text):
		date = now.AddDate(0, 0, (7-int(now.Weekday()))%7)
		clock = "23:59"
		meaning = "End of week means this week's Sunday at 11:59 PM."
	case regexp.MustCompile(`^(?:(?:the )?end of (?:the |this )?month|eom)$`).MatchString(text):
		date = time.Date(now.Year(), now.Month()+1, 0, 12, 0, 0, 0, now.Location())
		clock = "23:59"
		meaning = "End of month means the last calendar day at 11:59 PM."
	case regexp.MustCompile(`^(?:close of business|cob|end of (?:the )?(?:business|work) day|(?:the )?end of (?:the )?workday)$`).MatchString(text):
		clock = "17:00"
		meaning = "Close of business means today at 5 PM; this default does not use a business or holiday calendar."
	case regexp.MustCompile(`^tomorrow (?:close of business|cob|end of (?:the )?(?:business|work) day)$`).MatchString(text):
		date = now.AddDate(0, 0, 1)
		clock = "17:00"
		meaning = "Tomorrow's close of business means tomorrow at 5 PM."
	}
	if clock != "" {
		return date.Format("2006-01-02") + " at " + clock, []string{meaning}, true
	}
	// Named periods use visible conventional clocks only when no clock was given.
	if m := regexp.MustCompile(`^(?:(today|tomorrow|day after tomorrow|this|next) )?(morning|afternoon|evening|night|tonight)$`).FindStringSubmatch(text); m != nil {
		clocks := map[string]string{"morning": "09:00", "afternoon": "15:00", "evening": "18:00", "night": "20:00", "tonight": "20:00"}
		prefix := m[1]
		period := m[2]
		fixed := prefix == "today" || prefix == "this" || period == "tonight"
		if prefix == "this" || period == "tonight" {
			prefix = "today"
		}
		if prefix == "next" {
			prefix = "tomorrow"
			fixed = true
		}
		if prefix != "" {
			prefix += " "
		}
		assumptions = append(assumptions, fmt.Sprintf("%s uses %s local time; review this default before saving.", period, clocks[period]))
		return prefix + "at " + clocks[period], assumptions, fixed
	}
	return text, assumptions, false
}
