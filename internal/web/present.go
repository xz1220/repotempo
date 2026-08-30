package web

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type chart struct {
	ID          string
	Title       string
	Description string
	HasData     bool
	Segments    []chartSegment
	Points      []chartPoint
	StartLabel  string
	EndLabel    string
	MinLabel    string
	MaxLabel    string
}

type chartSegment struct {
	Points string
}

type chartPoint struct {
	X     string
	Y     string
	Label string
	Value string
}

type chartInput struct {
	Date  time.Time
	Value *int64
}

func snapshotStarChart(history []SnapshotPoint) chart {
	input := make([]chartInput, 0, len(history))
	for _, point := range history {
		input = append(input, chartInput{Date: point.Date, Value: point.Stars})
	}
	return makeChart(input, false, "Star history", "Successful daily snapshots. Gaps mark dates without a valid star observation.")
}

func snapshotRankChart(history []SnapshotPoint) chart {
	input := make([]chartInput, 0, len(history))
	for _, point := range history {
		input = append(input, chartInput{Date: point.Date, Value: point.OSSRank})
	}
	return makeChart(input, true, "OSS Insight Today rank", "Recorded Today ranks only. Rank 1 appears at the top.")
}

func trendChart(history []TrendPoint, title string) chart {
	input := make([]chartInput, 0, len(history))
	for _, point := range history {
		input = append(input, chartInput{Date: point.Date, Value: point.Stars})
	}
	return makeChart(input, false, title, "Aggregated successful snapshots. Missing observations are not replaced with zero.")
}

func makeChart(input []chartInput, inverse bool, title, description string) chart {
	result := chart{ID: chartID(title), Title: title, Description: description}
	if len(input) == 0 {
		return result
	}

	var minValue, maxValue int64
	valid := 0
	for _, point := range input {
		if point.Value == nil {
			continue
		}
		if valid == 0 || *point.Value < minValue {
			minValue = *point.Value
		}
		if valid == 0 || *point.Value > maxValue {
			maxValue = *point.Value
		}
		valid++
	}
	if valid == 0 {
		return result
	}

	result.HasData = true
	result.StartLabel = input[0].Date.Format("2006-01-02")
	result.EndLabel = input[len(input)-1].Date.Format("2006-01-02")
	result.MinLabel = formatInt(minValue)
	result.MaxLabel = formatInt(maxValue)
	if inverse {
		result.MinLabel, result.MaxLabel = result.MaxLabel, result.MinLabel
	}

	const (
		left   = 16.0
		top    = 14.0
		width  = 768.0
		height = 184.0
	)
	valueRange := float64(maxValue - minValue)
	if valueRange == 0 {
		valueRange = 1
	}
	denominator := max(1, len(input)-1)
	current := make([]string, 0, len(input))
	flush := func() {
		if len(current) > 0 {
			result.Segments = append(result.Segments, chartSegment{Points: strings.Join(current, " ")})
			current = current[:0]
		}
	}
	for index, point := range input {
		if point.Value == nil {
			flush()
			continue
		}
		x := left + float64(index)/float64(denominator)*width
		ratio := float64(*point.Value-minValue) / valueRange
		if !inverse {
			ratio = 1 - ratio
		}
		y := top + ratio*height
		coordinates := fmt.Sprintf("%.2f,%.2f", x, y)
		current = append(current, coordinates)
		result.Points = append(result.Points, chartPoint{
			X:     fmt.Sprintf("%.2f", x),
			Y:     fmt.Sprintf("%.2f", y),
			Label: point.Date.Format("2006-01-02"),
			Value: formatInt(*point.Value),
		})
	}
	flush()
	return result
}

func chartID(title string) string {
	var result strings.Builder
	result.WriteString("chart-")
	lastHyphen := false
	for _, char := range strings.ToLower(title) {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			result.WriteRune(char)
			lastHyphen = false
			continue
		}
		if !lastHyphen {
			result.WriteByte('-')
			lastHyphen = true
		}
	}
	return strings.TrimSuffix(result.String(), "-")
}

func formatInt(value any) string {
	var number int64
	switch typed := value.(type) {
	case int:
		number = int64(typed)
	case int8:
		number = int64(typed)
	case int16:
		number = int64(typed)
	case int32:
		number = int64(typed)
	case int64:
		number = typed
	case uint:
		number = int64(typed)
	case uint8:
		number = int64(typed)
	case uint16:
		number = int64(typed)
	case uint32:
		number = int64(typed)
	case uint64:
		if typed > math.MaxInt64 {
			return "N/A"
		}
		number = int64(typed)
	default:
		return "N/A"
	}
	negative := number < 0
	digits := strconv.FormatInt(number, 10)
	if negative {
		digits = strings.TrimPrefix(digits, "-")
	}
	for index := len(digits) - 3; index > 0; index -= 3 {
		digits = digits[:index] + "," + digits[index:]
	}
	if negative {
		return "-" + digits
	}
	return digits
}

func formatIntPtr(value *int64) string {
	if value == nil {
		return "N/A"
	}
	return formatInt(*value)
}

func formatSigned(value *int64) string {
	if value == nil {
		return "N/A"
	}
	if *value > 0 {
		return "+" + formatInt(*value)
	}
	return formatInt(*value)
}

func formatDate(value time.Time, location *time.Location) string {
	if value.IsZero() {
		return "N/A"
	}
	return value.In(location).Format("2006-01-02")
}

func formatDatePtr(value *time.Time, location *time.Location) string {
	if value == nil {
		return "N/A"
	}
	return formatDate(*value, location)
}

func formatDateTime(value time.Time, location *time.Location) string {
	if value.IsZero() {
		return "N/A"
	}
	return value.In(location).Format("2006-01-02 15:04 MST")
}

func formatTimePtr(value *time.Time, location *time.Location) string {
	if value == nil {
		return "Running"
	}
	return formatDateTime(*value, location)
}

func coveragePercent(value SnapshotCoverage) float64 {
	if value.Percent != nil {
		return math.Max(0, math.Min(100, *value.Percent))
	}
	if value.Target == 0 {
		return 0
	}
	return math.Max(0, math.Min(100, float64(value.Successful)/float64(value.Target)*100))
}

func formatPercent(value *float64) string {
	if value == nil {
		return "N/A"
	}
	return fmt.Sprintf("%.1f%%", *value)
}

func formatDuration(start time.Time, finish *time.Time) string {
	if finish == nil || start.IsZero() {
		return "Running"
	}
	duration := finish.Sub(start)
	if duration < 0 {
		return "N/A"
	}
	if duration < time.Minute {
		return fmt.Sprintf("%ds", int(duration.Round(time.Second).Seconds()))
	}
	if duration < time.Hour {
		return fmt.Sprintf("%dm %ds", int(duration/time.Minute), int((duration % time.Minute).Round(time.Second).Seconds()))
	}
	return fmt.Sprintf("%dh %dm", int(duration/time.Hour), int((duration%time.Hour)/time.Minute))
}

func statusLabel(value string) string {
	switch strings.ToLower(value) {
	case "active":
		return "Active"
	case "paused":
		return "Paused"
	case "stopped":
		return "Stopped"
	case "archived":
		return "Archived"
	case "deleted":
		return "Deleted"
	case "private":
		return "Private"
	case "unreachable":
		return "Unreachable"
	case "running":
		return "Running"
	case "success":
		return "Success"
	case "partial":
		return "Partial"
	case "failed", "failure":
		return "Failed"
	default:
		if value == "" {
			return "Unknown"
		}
		return value
	}
}

func statusClass(value string) string {
	switch strings.ToLower(value) {
	case "active", "success":
		return "status-success"
	case "partial", "paused":
		return "status-warning"
	case "failed", "failure", "deleted", "private", "unreachable":
		return "status-danger"
	case "running":
		return "status-info"
	default:
		return "status-neutral"
	}
}

func sourceLabel(value string) string {
	switch strings.ToLower(value) {
	case "ossinsight":
		return "OSS Insight"
	case "github_search":
		return "GitHub Search"
	case "legacy":
		return "Legacy import"
	case "manual":
		return "Manual"
	default:
		if value == "" {
			return "Unknown"
		}
		return value
	}
}

func queryPath(path string, values url.Values) string {
	encoded := values.Encode()
	if encoded == "" {
		return path
	}
	return path + "?" + encoded
}
