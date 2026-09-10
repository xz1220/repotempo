package app

import (
	"context"
	"errors"
	"strings"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/web"
)

func (adapter WebAdapter) projectUserMetrics(ctx context.Context, metrics []web.RepositoryMetric) error {
	principal, scoped := domain.PrincipalFromContext(ctx)
	if !scoped {
		return nil
	}
	ids := make([]int64, 0, len(metrics))
	for _, metric := range metrics {
		ids = append(ids, metric.ID)
	}
	states := map[int64]domain.UserRepositoryState{}
	if principal.UserID > 0 {
		reader, ok := adapter.Store.(interface {
			UserRepositoryStates(context.Context, int64, []int64) (map[int64]domain.UserRepositoryState, error)
		})
		if !ok {
			return errors.New("personal repository storage unavailable")
		}
		var err error
		states, err = reader.UserRepositoryStates(ctx, principal.UserID, ids)
		if err != nil {
			return err
		}
	}
	for index := range metrics {
		value := &metrics[index]
		// Older analyses occasionally copied the private shared note. Suppress
		// those copies before replacing the legacy fields with this user's data.
		if analysis := value.Analysis; analysis != nil && (strings.EqualFold(strings.TrimSpace(analysis.Source), "manual_note") || strings.EqualFold(strings.TrimSpace(analysis.Source), "imported")) {
			note := strings.Join(strings.Fields(value.ManualNote), " ")
			fields := append([]string{analysis.SummaryZH, analysis.TechnicalNotes}, analysis.KeyPoints...)
			fields = append(fields, analysis.UseCases...)
			for _, field := range fields {
				if note != "" && strings.Join(strings.Fields(field), " ") == note {
					value.Analysis = nil
					break
				}
			}
		}
		state := states[value.ID]
		value.IsFocus, value.ManualNote = state.IsFocus, state.Note
	}
	return nil
}

func (adapter WebAdapter) projectUserRadar(ctx context.Context, rows []web.RadarRepository) error {
	metrics := make([]web.RepositoryMetric, len(rows))
	for index := range rows {
		metrics[index] = rows[index].RepositoryMetric
	}
	if err := adapter.projectUserMetrics(ctx, metrics); err != nil {
		return err
	}
	for index := range rows {
		rows[index].RepositoryMetric = metrics[index]
	}
	return nil
}
