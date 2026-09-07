package web

import (
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type chart struct {
	ID           string
	Title        string
	Description  string
	HasData      bool
	Segments     []chartSegment
	Points       []chartPoint
	Observations []chartObservation
	StartLabel   string
	EndLabel     string
	MinLabel     string
	MaxLabel     string
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

type chartObservation struct {
	Label   string
	Value   string
	Missing bool
}

type chartInput struct {
	Date  time.Time
	Value *int64
}

type growthBarChart struct {
	Title         string
	Description   string
	HasPoints     bool
	HasComparable bool
	Bars          []growthBar
	DateLabels    []chartAxisLabel
	AxisY         string
	TopLabel      string
	ZeroLabel     string
	BottomLabel   string
}

type growthBar struct {
	X       string
	Y       string
	Width   string
	Height  string
	CenterX string
	Class   string
	Missing bool
	Tooltip string
}

type chartAxisLabel struct {
	X      string
	Anchor string
	Text   string
}

type topicRankChart struct {
	HasData      bool
	Period       string
	PeriodLabel  string
	Options      []topicPeriodOption
	Items        []topicRankItem
	ShowBaseline bool
	BaselineX    string
}

type topicPeriodOption struct {
	Value  string
	Label  string
	URL    string
	Active bool
}

type topicRankItem struct {
	Rank            int
	Slug            string
	Name            string
	RepositoryCount int
	ComparableCount int
	CurrentStars    *int64
	Delta           *int64
	BarX            string
	BarWidth        string
	BarClass        string
	Tooltip         string
}

type discoveryMixChart struct {
	HasData  bool
	Total    int
	Segments []discoveryMixSegment
}

type discoveryMixSegment struct {
	Source          string
	SourceLabel     string
	RepositoryCount int
	Percent         string
	X               string
	Width           string
	ColorClass      string
	Ordinal         int
	Tooltip         string
}

func dashboardGrowthChart(history []GrowthPoint, localized localizer) growthBarChart {
	result := growthBarChart{
		Title:       localized.Text("home.growth_chart"),
		Description: localized.Text("home.growth_chart_help"),
		ZeroLabel:   "0",
	}
	if len(history) == 0 {
		return result
	}
	if len(history) > 30 {
		history = history[len(history)-30:]
	}
	result.HasPoints = true

	var maxPositive, minNegative int64
	for _, point := range history {
		if point.Delta == nil {
			continue
		}
		result.HasComparable = true
		maxPositive = max(maxPositive, *point.Delta)
		minNegative = min(minNegative, *point.Delta)
	}

	const (
		plotLeft   = 54.0
		plotTop    = 18.0
		plotWidth  = 826.0
		plotHeight = 176.0
	)
	plotBottom := plotTop + plotHeight
	zeroY := plotTop + plotHeight/2
	switch {
	case maxPositive > 0 && minNegative < 0:
		zeroY = plotTop + float64(maxPositive)/float64(maxPositive-minNegative)*plotHeight
		result.TopLabel = formatSigned(int64ValuePointer(maxPositive))
		result.BottomLabel = formatSigned(int64ValuePointer(minNegative))
	case maxPositive > 0:
		zeroY = plotBottom
		result.TopLabel = formatSigned(int64ValuePointer(maxPositive))
	case minNegative < 0:
		zeroY = plotTop
		result.BottomLabel = formatSigned(int64ValuePointer(minNegative))
	}
	result.AxisY = formatFloat(zeroY)

	step := plotWidth / float64(len(history))
	barWidth := math.Min(22, step*0.64)
	for index, point := range history {
		centerX := plotLeft + (float64(index)+0.5)*step
		bar := growthBar{
			X:       formatFloat(centerX - barWidth/2),
			Width:   formatFloat(barWidth),
			CenterX: formatFloat(centerX),
		}
		if point.Delta == nil {
			bar.Missing = true
			bar.Y = formatFloat(math.Max(plotTop+3, math.Min(plotBottom-3, zeroY)))
			bar.Class = "growth-bar-missing"
			bar.Tooltip = localized.Textf(
				"home.growth_tooltip_missing",
				point.Date.Format("2006-01-02"),
				formatInt(point.ComparableRepositoryCount),
				formatInt(point.GapSpanningRepositoryCount),
			)
			result.Bars = append(result.Bars, bar)
			continue
		}

		value := *point.Delta
		bar.Class = "growth-bar-positive"
		bar.Y = formatFloat(zeroY)
		bar.Height = "2"
		switch {
		case value > 0:
			height := float64(value) / float64(maxPositive) * (zeroY - plotTop)
			height = math.Max(1.5, height)
			bar.Y = formatFloat(zeroY - height)
			bar.Height = formatFloat(height)
		case value < 0:
			height := float64(value) / float64(minNegative) * (plotBottom - zeroY)
			height = math.Max(1.5, height)
			bar.Height = formatFloat(height)
			bar.Class = "growth-bar-negative"
		default:
			bar.Y = formatFloat(zeroY - 1)
			bar.Class = "growth-bar-zero"
		}
		bar.Tooltip = localized.Textf(
			"home.growth_tooltip",
			point.Date.Format("2006-01-02"),
			formatSigned(point.Delta),
			formatInt(point.ComparableRepositoryCount),
			formatInt(point.GapSpanningRepositoryCount),
		)
		result.Bars = append(result.Bars, bar)
	}

	labelIndexes := chartLabelIndexes(len(history))
	for labelIndex, index := range labelIndexes {
		anchor := "middle"
		if labelIndex == 0 {
			anchor = "start"
		} else if labelIndex == len(labelIndexes)-1 {
			anchor = "end"
		}
		x := plotLeft + float64(index)/float64(max(1, len(history)-1))*plotWidth
		result.DateLabels = append(result.DateLabels, chartAxisLabel{
			X:      formatFloat(x),
			Anchor: anchor,
			Text:   history[index].Date.Format("01-02"),
		})
	}
	return result
}

func chartLabelIndexes(length int) []int {
	if length <= 1 {
		return []int{0}
	}
	count := min(5, length)
	result := make([]int, 0, count)
	for index := 0; index < count; index++ {
		point := int(math.Round(float64(index) * float64(length-1) / float64(count-1)))
		if len(result) == 0 || result[len(result)-1] != point {
			result = append(result, point)
		}
	}
	return result
}

func makeTopicRankChart(items []TopicMetric, period string, localized localizer) topicRankChart {
	result := topicRankChart{Period: period, PeriodLabel: localized.Text("period." + period)}
	parentSlugs := make(map[string]struct{})
	for _, item := range items {
		if item.ParentSlug != "" {
			parentSlugs[item.ParentSlug] = struct{}{}
		}
	}
	for _, item := range items {
		if _, isParent := parentSlugs[item.Slug]; isParent {
			continue
		}
		delta := topicDelta(item, period)
		if delta == nil {
			continue
		}
		result.Items = append(result.Items, topicRankItem{
			Slug:            item.Slug,
			Name:            item.Name,
			RepositoryCount: item.RepositoryCount,
			ComparableCount: topicComparableCount(item, period),
			CurrentStars:    item.CurrentStars,
			Delta:           delta,
		})
	}
	sort.SliceStable(result.Items, func(left, right int) bool {
		leftDelta := *result.Items[left].Delta
		rightDelta := *result.Items[right].Delta
		if leftDelta != rightDelta {
			return leftDelta > rightDelta
		}
		leftStars := pointerValue(result.Items[left].CurrentStars)
		rightStars := pointerValue(result.Items[right].CurrentStars)
		if leftStars != rightStars {
			return leftStars > rightStars
		}
		return strings.ToLower(result.Items[left].Name) < strings.ToLower(result.Items[right].Name)
	})
	if len(result.Items) > 8 {
		result.Items = result.Items[:8]
	}
	if len(result.Items) == 0 {
		return result
	}
	result.HasData = true

	var maxPositive, minNegative int64
	for _, item := range result.Items {
		maxPositive = max(maxPositive, *item.Delta)
		minNegative = min(minNegative, *item.Delta)
	}
	baseline := 0.0
	switch {
	case maxPositive > 0 && minNegative < 0:
		baseline = float64(-minNegative) / float64(maxPositive-minNegative) * 1000
		result.ShowBaseline = true
	case minNegative < 0:
		baseline = 1000
	}
	result.BaselineX = formatFloat(baseline)
	positiveSpace := 1000 - baseline
	negativeSpace := baseline
	for index := range result.Items {
		item := &result.Items[index]
		item.Rank = index + 1
		item.BarClass = "topic-rank-positive"
		item.BarX = formatFloat(baseline)
		item.BarWidth = "2"
		value := *item.Delta
		switch {
		case value > 0:
			item.BarWidth = formatFloat(math.Max(2, float64(value)/float64(maxPositive)*positiveSpace))
		case value < 0:
			width := math.Max(2, float64(value)/float64(minNegative)*negativeSpace)
			item.BarX = formatFloat(baseline - width)
			item.BarWidth = formatFloat(width)
			item.BarClass = "topic-rank-negative"
		default:
			item.BarX = formatFloat(math.Max(0, baseline-1))
			item.BarClass = "topic-rank-zero"
		}
		item.Tooltip = localized.Textf(
			"topics.ranking_tooltip",
			item.Name,
			result.PeriodLabel,
			formatSigned(item.Delta),
			formatIntPtrLocalized(item.CurrentStars, localized.Text("page.not_available")),
			formatInt(item.ComparableCount),
			formatInt(item.RepositoryCount),
		)
	}
	return result
}

func topicComparableCount(item TopicMetric, period string) int {
	switch period {
	case "1d":
		return item.Comparable1D
	case "30d":
		return item.Comparable30D
	default:
		return item.Comparable7D
	}
}

func topicDelta(item TopicMetric, period string) *int64 {
	switch period {
	case "1d":
		return item.Delta1D
	case "30d":
		return item.Delta30D
	default:
		return item.Delta7D
	}
}

func makeDiscoveryMixChart(sources []DiscoverySource, localized localizer) discoveryMixChart {
	filtered := make([]DiscoverySource, 0, len(sources))
	for _, source := range sources {
		source.RepositoryCount = max(0, source.RepositoryCount)
		filtered = append(filtered, source)
	}
	sort.SliceStable(filtered, func(left, right int) bool {
		if filtered[left].RepositoryCount != filtered[right].RepositoryCount {
			return filtered[left].RepositoryCount > filtered[right].RepositoryCount
		}
		return filtered[left].Source < filtered[right].Source
	})

	result := discoveryMixChart{}
	for _, source := range filtered {
		result.Total += source.RepositoryCount
	}
	if result.Total == 0 {
		return result
	}
	result.HasData = true
	cumulative := 0
	for index, source := range filtered {
		start := float64(cumulative) / float64(result.Total) * 1000
		cumulative += source.RepositoryCount
		end := float64(cumulative) / float64(result.Total) * 1000
		label := localized.SourceLabel(source.Source)
		percent := float64(source.RepositoryCount) / float64(result.Total) * 100
		segment := discoveryMixSegment{
			Source:          source.Source,
			SourceLabel:     label,
			RepositoryCount: source.RepositoryCount,
			Percent:         fmt.Sprintf("%.1f%%", percent),
			X:               formatFloat(start),
			Width:           formatFloat(end - start),
			ColorClass:      sourceColorClass(source.Source),
			Ordinal:         index + 1,
		}
		segment.Tooltip = localized.Textf(
			"discoveries.source_tooltip",
			label,
			formatInt(source.RepositoryCount),
			segment.Percent,
		)
		result.Segments = append(result.Segments, segment)
	}
	return result
}

func sourceColorClass(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "ossinsight":
		return "source-fill-1"
	case "github_search":
		return "source-fill-2"
	case "github_trending":
		return "source-fill-5"
	case "legacy":
		return "source-fill-3"
	case "manual":
		return "source-fill-4"
	}
	var hash uint32 = 2166136261
	for _, char := range []byte(strings.ToLower(source)) {
		hash ^= uint32(char)
		hash *= 16777619
	}
	return fmt.Sprintf("source-fill-%d", hash%6+1)
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', 2, 64)
}

func pointerValue(value *int64) int64 {
	if value == nil {
		return math.MinInt64
	}
	return *value
}

func int64ValuePointer(value int64) *int64 {
	return &value
}

func snapshotStarChart(history []SnapshotPoint, localized localizer) chart {
	input := make([]chartInput, 0, len(history))
	for _, point := range history {
		input = append(input, chartInput{Date: point.Date, Value: point.Stars})
	}
	return makeChart(input, false, localized.Text("chart.star_history"), localized.Text("chart.star_history_help"))
}

func snapshotRankChart(history []SnapshotPoint, localized localizer) chart {
	input := make([]chartInput, 0, len(history))
	for _, point := range history {
		input = append(input, chartInput{Date: point.Date, Value: point.OSSRank})
	}
	return makeChart(input, true, localized.Text("chart.oss_rank"), localized.Text("chart.oss_rank_help"))
}

func trendChart(history []TrendPoint, title, description string) chart {
	input := make([]chartInput, 0, len(history))
	for _, point := range history {
		input = append(input, chartInput{Date: point.Date, Value: point.Stars})
	}
	return makeChart(input, false, title, description)
}

func makeChart(input []chartInput, inverse bool, title, description string) chart {
	result := chart{ID: chartID(title), Title: title, Description: description}
	if len(input) == 0 {
		return result
	}

	var minValue, maxValue int64
	valid := 0
	for _, point := range input {
		observation := chartObservation{Label: point.Date.Format("2006-01-02"), Missing: point.Value == nil}
		if point.Value != nil {
			observation.Value = formatInt(*point.Value)
		}
		result.Observations = append(result.Observations, observation)
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
	dateSpan := input[len(input)-1].Date.Sub(input[0].Date)
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
		if index > 0 && point.Date.Sub(input[index-1].Date) > 36*time.Hour {
			flush()
		}
		xRatio := float64(index) / float64(denominator)
		if dateSpan > 0 {
			xRatio = float64(point.Date.Sub(input[0].Date)) / float64(dateSpan)
		}
		x := left + xRatio*width
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
	return formatIntPtrLocalized(value, "N/A")
}

func formatIntPtrLocalized(value *int64, unavailable string) string {
	if value == nil {
		return unavailable
	}
	return formatInt(*value)
}

func formatSigned(value *int64) string {
	return formatSignedLocalized(value, "N/A")
}

func formatSignedLocalized(value *int64, unavailable string) string {
	if value == nil {
		return unavailable
	}
	if *value > 0 {
		return "+" + formatInt(*value)
	}
	return formatInt(*value)
}

func formatDecimalSignedLocalized(value *float64, unavailable string) string {
	if value == nil {
		return unavailable
	}
	if *value > 0 {
		return fmt.Sprintf("+%.1f", *value)
	}
	return fmt.Sprintf("%.1f", *value)
}

func deltaClass(value *int64) string {
	if value == nil || *value == 0 {
		return "metric-neutral"
	}
	if *value > 0 {
		return "metric-positive"
	}
	return "metric-negative"
}

func floatDeltaClass(value *float64) string {
	if value == nil || *value == 0 {
		return "metric-neutral"
	}
	if *value > 0 {
		return "metric-positive"
	}
	return "metric-negative"
}

func formatDate(value time.Time, location *time.Location) string {
	return formatDateLocalized(value, location, "N/A")
}

func formatDateLocalized(value time.Time, location *time.Location, unavailable string) string {
	if value.IsZero() {
		return unavailable
	}
	return value.In(location).Format("2006-01-02")
}

func formatDatePtr(value *time.Time, location *time.Location) string {
	return formatDatePtrLocalized(value, location, "N/A")
}

func formatDatePtrLocalized(value *time.Time, location *time.Location, unavailable string) string {
	if value == nil {
		return unavailable
	}
	return formatDateLocalized(*value, location, unavailable)
}

func formatDateTime(value time.Time, location *time.Location) string {
	return formatDateTimeLocalized(value, location, "N/A")
}

func formatDateTimeLocalized(value time.Time, location *time.Location, unavailable string) string {
	if value.IsZero() {
		return unavailable
	}
	return value.In(location).Format("2006-01-02 15:04 MST")
}

func formatTimePtr(value *time.Time, location *time.Location, runningLabel string) string {
	if value == nil {
		return runningLabel
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
	return formatPercentLocalized(value, "N/A")
}

func formatPercentLocalized(value *float64, unavailable string) string {
	if value == nil {
		return unavailable
	}
	return fmt.Sprintf("%.1f%%", *value)
}

func formatDuration(start time.Time, finish *time.Time, runningLabel, unavailable string) string {
	if finish == nil || start.IsZero() {
		return runningLabel
	}
	duration := finish.Sub(start)
	if duration < 0 {
		return unavailable
	}
	if duration < time.Minute {
		return fmt.Sprintf("%ds", int(duration.Round(time.Second).Seconds()))
	}
	if duration < time.Hour {
		return fmt.Sprintf("%dm %ds", int(duration/time.Minute), int((duration % time.Minute).Round(time.Second).Seconds()))
	}
	return fmt.Sprintf("%dh %dm", int(duration/time.Hour), int((duration%time.Hour)/time.Minute))
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

func queryPath(path string, values url.Values) string {
	encoded := values.Encode()
	if encoded == "" {
		return path
	}
	return path + "?" + encoded
}
