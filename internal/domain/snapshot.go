package domain

import "time"

type FetchStatus string

const (
	FetchSuccess FetchStatus = "success"
	FetchFailed  FetchStatus = "failed"
)

func (status FetchStatus) Valid() bool {
	switch status {
	case FetchSuccess, FetchFailed:
		return true
	default:
		return false
	}
}

type DailySnapshot struct {
	RepositoryID   int64       `json:"repository_id"`
	SnapshotDate   Date        `json:"snapshot_date"`
	CapturedAt     time.Time   `json:"captured_at"`
	StarCount      *int64      `json:"star_count"`
	FetchStatus    FetchStatus `json:"fetch_status"`
	HTTPStatus     *int        `json:"http_status,omitempty"`
	ErrorCode      string      `json:"error_code,omitempty"`
	OSSTodayRank   *int        `json:"oss_today_rank,omitempty"`
	OSSWindowStars *int64      `json:"oss_window_stars,omitempty"`
	OSSTotalScore  *float64    `json:"oss_total_score,omitempty"`
	CreatedAt      time.Time   `json:"created_at"`
}

type SnapshotWriteDisposition string

const (
	SnapshotInserted         SnapshotWriteDisposition = "inserted"
	SnapshotFailureRepaired  SnapshotWriteDisposition = "failure_repaired"
	SnapshotSuccessProtected SnapshotWriteDisposition = "success_protected"
	SnapshotUnchanged        SnapshotWriteDisposition = "unchanged"
)

type SnapshotWriteResult struct {
	Disposition SnapshotWriteDisposition `json:"disposition"`
	Snapshot    DailySnapshot            `json:"snapshot"`
}

type SnapshotFilter struct {
	From         Date
	Through      Date
	FailuresOnly bool
	Limit        int
	Offset       int
}
