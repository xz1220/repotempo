CREATE INDEX IF NOT EXISTS idx_snapshots_success_by_date
    ON daily_snapshots(snapshot_date, repository_id, star_count)
    WHERE fetch_status = 'success';

CREATE INDEX IF NOT EXISTS idx_snapshots_success_by_repository
    ON daily_snapshots(repository_id, snapshot_date DESC, star_count)
    WHERE fetch_status = 'success';
