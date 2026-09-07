package web

import (
	"strconv"
	"testing"
)

func TestChartUsesCalendarSpacingAndBreaksMissingDays(t *testing.T) {
	c := makeChart([]chartInput{
		{Date: mustDate("2026-09-01"), Value: int64Pointer(100)},
		{Date: mustDate("2026-09-02"), Value: int64Pointer(110)},
		{Date: mustDate("2026-09-05"), Value: int64Pointer(140)},
	}, false, "Star history", "Real observations")
	if len(c.Segments) != 2 {
		t.Fatalf("segments=%d, missing dates must break the curve", len(c.Segments))
	}
	if len(c.Points) != 3 || c.Points[1].X != "208.00" || c.Points[2].X != "784.00" {
		t.Fatalf("points do not follow calendar time: %+v", c.Points)
	}
}

func TestTinyRadarMovementHasDistinctAxesAndHonestScale(t *testing.T) {
	c, _, _ := radarHistoryChart([]RadarHistoryPoint{
		{Date: mustDate("2026-09-01"), Index: float64Pointer(100), CohortCount: 10},
		{Date: mustDate("2026-09-02"), Index: float64Pointer(100.04), CohortCount: 10},
	}, newLocalizer(localeChinese))
	if c.MinLabel == c.MaxLabel {
		t.Fatal("small movement must not share identical axis labels")
	}
	y1, _ := strconv.ParseFloat(c.Points[0].Y, 64)
	y2, _ := strconv.ParseFloat(c.Points[1].Y, 64)
	if y1-y2 > 40 {
		t.Fatalf("tiny movement occupies too much chart height: %f", y1-y2)
	}
}
