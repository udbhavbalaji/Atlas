package interpret

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

var durationPattern = regexp.MustCompile(`(?i)^in\s+(\d+|an?|one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve)\s+(seconds?|minutes?|hours?|days?|weeks?)$`)
var isoDate = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)
var monthDate = regexp.MustCompile(`(?i)\b(jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|jun(?:e)?|jul(?:y)?|aug(?:ust)?|sep(?:tember)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)\s+(\d{1,2})(?:st|nd|rd|th)?(?:,?\s+(\d{4}))?\b`)
var weekdayPattern = regexp.MustCompile(`(?i)\b(monday|tuesday|wednesday|thursday|friday|saturday|sunday)\b`)
var dailyPattern = regexp.MustCompile(`(?i)\b(?:every day|daily)\b`)
var weeklyPattern = regexp.MustCompile(`(?i)\bweekly\b`)
var clockPattern = regexp.MustCompile(`(?i)\b(?:at\s+)?(\d{1,2})(?::(\d{2}))?\s*(a\.?m\.?|p\.?m\.?)?\b`)
var words = map[string]int{"a": 1, "an": 1, "one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10, "eleven": 11, "twelve": 12}
var weekdays = map[string]time.Weekday{"sunday": time.Sunday, "monday": time.Monday, "tuesday": time.Tuesday, "wednesday": time.Wednesday, "thursday": time.Thursday, "friday": time.Friday, "saturday": time.Saturday}
var months = map[string]time.Month{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}

func parseTime(raw string, now time.Time, loc *time.Location, field string) (string, string, []string, []Question) {
	text := strings.Trim(strings.ToLower(strings.TrimSpace(raw)), ".,; ")
	assumptions := []string{"Dates and clock times use " + loc.String() + "."}
	repeat := ""
	fail := func(code, message string) (string, string, []string, []Question) {
		return "", repeat, assumptions, []Question{{field, code, message}}
	}
	text = regexp.MustCompile(`^(?:by|due)(?:\s+on)?\s+`).ReplaceAllString(text, "")
	text, phraseAssumptions, fixedPhrase := normalizeCommonTime(text, now)
	assumptions = append(assumptions, phraseAssumptions...)
	if strings.HasPrefix(text, "tonight ") {
		text = "today evening " + strings.TrimPrefix(text, "tonight ")
	}
	text = regexp.MustCompile(`\bat (one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve)\b`).ReplaceAllStringFunc(text, func(s string) string { return "at " + strconv.Itoa(words[strings.TrimPrefix(s, "at ")]) })
	if m := durationPattern.FindStringSubmatch(text); m != nil {
		n, ok := words[m[1]]
		if !ok {
			parsed, e := strconv.ParseInt(m[1], 10, 32)
			if e != nil || parsed < 1 || parsed > 63072000 {
				return fail("invalid_interval", "Choose a positive interval of no more than two years.")
			}
			n = int(parsed)
		}
		var at time.Time
		switch {
		case strings.HasPrefix(m[2], "second"):
			if n > 63072000 {
				return fail("invalid_interval", "Choose an interval of no more than two years.")
			}
			at = now.Add(time.Duration(n) * time.Second)
		case strings.HasPrefix(m[2], "minute"):
			if n > 1051200 {
				return fail("invalid_interval", "Choose an interval of no more than two years.")
			}
			at = now.Add(time.Duration(n) * time.Minute)
		case strings.HasPrefix(m[2], "hour"):
			if n > 17520 {
				return fail("invalid_interval", "Choose an interval of no more than two years.")
			}
			at = now.Add(time.Duration(n) * time.Hour)
		default:
			days := n
			if strings.HasPrefix(m[2], "week") {
				days *= 7
			}
			if days > 730 {
				return fail("invalid_interval", "Choose an interval of no more than two years.")
			}
			date := now.AddDate(0, 0, days)
			candidates := wallTimes(date.Year(), date.Month(), date.Day(), now.Hour(), now.Minute(), now.Second(), now.Nanosecond(), loc)
			if len(candidates) != 1 {
				return fail("dst_clock", "That calendar interval reaches a missing or repeated local time. Choose an explicit time below.")
			}
			at = candidates[0]
			assumptions = append(assumptions, "Days and weeks preserve the local clock; hours and minutes are elapsed time.")
		}
		return at.UTC().Format(time.RFC3339Nano), repeat, assumptions, nil
	}
	if strings.Contains(text, "every other") || regexp.MustCompile(`\b(?:monthly|yearly|annually|fortnightly|biweekly|weekdays|weekends|month|months|year|years)\b`).MatchString(text) {
		return fail("unsupported_repeat", "Only daily and weekly repeats are supported. Choose a supported repeat and first time below.")
	}
	if dailyPattern.MatchString(text) {
		repeat = "daily"
		text = dailyPattern.ReplaceAllString(text, "")
	}
	if weeklyPattern.MatchString(text) {
		if repeat != "" {
			return fail("conflicting_repeat", "Choose one repeat rule.")
		}
		repeat = "weekly"
		text = weeklyPattern.ReplaceAllString(text, "")
	}
	year, month, day := now.Date()
	dateExplicit := false
	weekday := -1
	monthWithoutYear := false
	dates := 0
	if matches := isoDate.FindAllString(text, -1); len(matches) > 0 {
		if len(matches) != 1 {
			return fail("multiple_dates", "Choose one date for this field.")
		}
		date, e := time.Parse("2006-01-02", matches[0])
		if e != nil {
			return fail("invalid_date", "That calendar date does not exist.")
		}
		year, month, day = date.Date()
		dateExplicit = true
		dates++
		text = strings.Replace(text, matches[0], "", 1)
	}
	if m := monthDate.FindAllStringSubmatch(text, -1); len(m) > 0 {
		if len(m) != 1 {
			return fail("multiple_dates", "Choose one date for this field.")
		}
		month = months[m[0][1][:3]]
		day, _ = strconv.Atoi(m[0][2])
		if m[0][3] != "" {
			year, _ = strconv.Atoi(m[0][3])
		} else {
			monthWithoutYear = true
		}
		dateExplicit = true
		dates++
		text = strings.Replace(text, m[0][0], "", 1)
	}
	if strings.Contains(text, "day after tomorrow") {
		date := now.AddDate(0, 0, 2)
		year, month, day = date.Date()
		dates++
		dateExplicit = true
		text = strings.Replace(text, "day after tomorrow", "", 1)
	} else if strings.Contains(text, "tomorrow") {
		date := now.AddDate(0, 0, 1)
		year, month, day = date.Date()
		dates++
		dateExplicit = true
		text = strings.Replace(text, "tomorrow", "", 1)
	}
	if strings.Contains(text, "today") {
		year, month, day = now.Date()
		dates++
		dateExplicit = true
		text = strings.Replace(text, "today", "", 1)
	}
	if m := weekdayPattern.FindAllString(text, -1); len(m) > 0 {
		if len(m) != 1 {
			return fail("multiple_dates", "Choose one weekday.")
		}
		weekday = int(weekdays[m[0]])
		dates++
		if strings.Contains(text, "every ") {
			if repeat == "daily" {
				return fail("conflicting_repeat", "Choose daily or a weekly weekday, not both.")
			}
			repeat = "weekly"
		}
		text = strings.Replace(text, m[0], "", 1)
	}
	if dates > 1 {
		return fail("multiple_dates", "Choose one date or weekday for this field.")
	}
	period := ""
	periodPattern := regexp.MustCompile(`\b(?:in (?:the )?)?(morning|afternoon|evening)\b`)
	if m := periodPattern.FindAllStringSubmatch(text, -1); len(m) > 0 {
		if len(m) != 1 {
			return fail("conflicting_period", "Choose one part of the day.")
		}
		period = m[0][1]
		text = strings.Replace(text, m[0][0], "", 1)
	}
	hour, minute := 0, 0
	clock := false
	if strings.Contains(text, "noon") {
		hour = 12
		clock = true
		text = strings.Replace(text, "noon", "", 1)
	}
	if strings.Contains(text, "midnight") {
		if clock {
			return fail("multiple_times", "Choose one clock time.")
		}
		hour = 0
		clock = true
		text = strings.Replace(text, "midnight", "", 1)
		assumptions = append(assumptions, "Midnight means 00:00 at the start of the selected date.")
	}
	if m := clockPattern.FindAllStringSubmatch(text, -1); len(m) > 0 {
		if len(m) != 1 || clock {
			return fail("multiple_times", "Choose one clock time.")
		}
		clock = true
		hour, _ = strconv.Atoi(m[0][1])
		if m[0][2] != "" {
			minute, _ = strconv.Atoi(m[0][2])
		}
		meridiem := strings.ReplaceAll(m[0][3], ".", "")
		if minute > 59 || hour > 23 || (meridiem != "" && (hour < 1 || hour > 12)) {
			return fail("invalid_clock", "That clock time does not exist.")
		}
		if meridiem != "" {
			hour %= 12
			if meridiem == "pm" {
				hour += 12
			}
		} else if period != "" && hour >= 1 && hour <= 12 {
			hour %= 12
			if period != "morning" {
				hour += 12
			}
			assumptions = append(assumptions, "The part of day resolves AM/PM: "+period+".")
		} else if hour >= 1 && hour <= 12 && !strings.HasPrefix(m[0][1], "0") {
			return fail("ambiguous_clock", "Is that AM or PM? Set an explicit time below, or include am/pm in the sentence.")
		}
		text = strings.Replace(text, m[0][0], "", 1)
	}
	if !clock {
		return fail("missing_clock", "What time? Include am/pm, noon, midnight, or an explicit 24-hour clock.")
	}
	if (period == "morning" && hour >= 12) || (period == "afternoon" && (hour < 12 || hour >= 18)) || (period == "evening" && hour < 17) {
		return fail("conflicting_clock", "The clock and part of day disagree. Choose an explicit time.")
	}
	text = regexp.MustCompile(`\b(?:at|on|next|every)\b`).ReplaceAllString(text, "")
	text = strings.Trim(text, ".,; ")
	if text != "" {
		return fail("unsupported_time", "I could not resolve all of the time wording: "+strings.TrimSpace(raw)+". Set the exact date, time, and repeat below.")
	}
	if year < 1 || year > 9999 {
		return fail("invalid_date", "Choose a supported calendar year.")
	}
	if weekday >= 0 {
		days := (weekday - int(now.Weekday()) + 7) % 7
		date := now.AddDate(0, 0, days)
		year, month, day = date.Date()
		assumptions = append(assumptions, "Weekday names, including 'next', mean the next future occurrence of that weekday.")
	}
	// Reject normalized dates (February 30 etc.) before resolving local time.
	date := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	if date.Year() != year || date.Month() != month || date.Day() != day {
		return fail("invalid_date", "That calendar date does not exist.")
	}
	candidates := wallTimes(year, month, day, hour, minute, 0, 0, loc)
	if len(candidates) == 0 {
		return fail("nonexistent_local_time", "That local time does not exist because of a timezone clock change. Choose another time.")
	}
	if len(candidates) > 1 {
		return fail("ambiguous_local_time", "That local time occurs twice because of a timezone clock change. Use the explicit fields to choose the earlier instant, or an offset timestamp through the API.")
	}
	at := candidates[0]
	if fixedPhrase && !at.After(now) {
		return fail("named_time_passed", "The proposed named time has already passed. Choose tomorrow or an explicit future time.")
	}
	if monthWithoutYear && !at.After(now) {
		year++
		candidates = wallTimes(year, month, day, hour, minute, 0, 0, loc)
		if len(candidates) != 1 {
			return fail("invalid_date", "Choose an explicit date and year.")
		}
		at = candidates[0]
		assumptions = append(assumptions, "A month/day without a year uses its next future occurrence.")
	}
	if !at.After(now) && (weekday >= 0 || (!dateExplicit && repeat != "") || (!dateExplicit && repeat == "")) {
		days := 1
		if weekday >= 0 || repeat == "weekly" {
			days = 7
		}
		date = at.In(loc).AddDate(0, 0, days)
		candidates = wallTimes(date.Year(), date.Month(), date.Day(), hour, minute, 0, 0, loc)
		if len(candidates) != 1 {
			return fail("dst_clock", "The next occurrence reaches a missing or repeated local time. Choose an explicit first date and time.")
		}
		at = candidates[0]
	}
	if !dateExplicit && weekday < 0 {
		assumptions = append(assumptions, "A clock without a date uses its next future occurrence.")
	}
	return at.UTC().Format(time.RFC3339Nano), repeat, assumptions, nil
}

// Enumerate actual local-clock matches; time.Date alone normalizes DST gaps and
// silently chooses one side of a repeated clock. Never do that for a sentence.
func wallTimes(year int, month time.Month, day, hour, minute, second, nanosecond int, loc *time.Location) []time.Time {
	wall := time.Date(year, month, day, hour, minute, second, nanosecond, time.UTC)
	if wall.Year() != year || wall.Month() != month || wall.Day() != day {
		return nil
	}
	offsets := map[int]bool{}
	for h := -48; h <= 48; h += 6 {
		_, offset := wall.Add(time.Duration(h) * time.Hour).In(loc).Zone()
		offsets[offset] = true
	}
	list := []time.Time{}
	for offset := range offsets {
		candidate := wall.Add(-time.Duration(offset) * time.Second)
		local := candidate.In(loc)
		if local.Year() == year && local.Month() == month && local.Day() == day && local.Hour() == hour && local.Minute() == minute && local.Second() == second {
			list = append(list, candidate)
		}
	}
	return list
}
