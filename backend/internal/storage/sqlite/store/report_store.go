package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// CreateReport atomically persists one validated pending report and all outputs.
func (s *Store) CreateReport(ctx context.Context, rec domain.ReportRecord) (domain.ReportRecord, error) {
	if err := rec.Validate(); err != nil {
		return domain.ReportRecord{}, err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var row gen.Report
	duplicateTerminal := false
	err := s.inTxDB(ctx, "create report", func(q *gen.Queries, tx gen.DBTX) error {
		if rec.TerminalOutcome != "" {
			var existingID, existingOutcome string
			err := tx.QueryRowContext(ctx, `SELECT report_id, outcome FROM report_terminal_completions WHERE session_id = ?`, string(rec.SessionID)).Scan(&existingID, &existingOutcome)
			if err == nil {
				if existingOutcome != string(rec.TerminalOutcome) {
					return domain.ErrTerminalReportConflict
				}
				row, err = q.GetReport(ctx, existingID)
				if err != nil {
					return err
				}
				duplicateTerminal = true
				return nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		var err error
		row, err = q.CreateReport(ctx, gen.CreateReportParams{
			ID: rec.ID, SessionID: string(rec.SessionID), ProjectID: string(rec.ProjectID),
			State: string(rec.State), Note: rec.Note, Message: rec.Message,
			CreatedAt: rec.CreatedAt, AvailableAt: rec.AvailableAt,
			SettlementDeadline: nullTime(rec.SettlementDeadline), RepeatCount: rec.RepeatCount,
			TerminalOutcome: string(rec.TerminalOutcome),
		})
		if err != nil {
			return err
		}
		for i, output := range rec.Outputs {
			if err := q.CreateReportOutput(ctx, gen.CreateReportOutputParams{
				ReportID: rec.ID, Position: int64(i), Kind: string(output.Kind),
				Reference: output.Reference, Label: output.Label,
			}); err != nil {
				return err
			}
		}
		if rec.TerminalOutcome != "" {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO report_terminal_completions (session_id, report_id, outcome, created_at) VALUES (?, ?, ?, ?)`, string(rec.SessionID), rec.ID, string(rec.TerminalOutcome), rec.CreatedAt); err != nil {
				return fmt.Errorf("record terminal report outcome: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return domain.ReportRecord{}, fmt.Errorf("create report %s: %w", rec.ID, err)
	}
	if duplicateTerminal {
		return s.reportWithOutputs(ctx, row)
	}
	created := reportFromGen(row)
	created.Outputs = append([]domain.ReportOutput(nil), rec.Outputs...)
	return created, nil
}

// GetReport returns one report by durable ID with its ordered outputs.
func (s *Store) GetReport(ctx context.Context, id string) (domain.ReportRecord, bool, error) {
	row, err := s.qr.GetReport(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ReportRecord{}, false, nil
	}
	if err != nil {
		return domain.ReportRecord{}, false, fmt.Errorf("get report %s: %w", id, err)
	}
	rec, err := s.reportWithOutputs(ctx, row)
	if err != nil {
		return domain.ReportRecord{}, false, err
	}
	return rec, true, nil
}

// ListReportsBySession returns a worker's reports in creation order.
func (s *Store) ListReportsBySession(ctx context.Context, id domain.SessionID) ([]domain.ReportRecord, error) {
	rows, err := s.qr.ListReportsBySession(ctx, string(id))
	if err != nil {
		return nil, fmt.Errorf("list reports: %w", err)
	}
	return s.reportsWithOutputs(ctx, rows)
}

// GetTerminalReportOutcome returns the first durable terminal judgment for a
// worker. Its session key prevents replayed reports from repeating effects.
func (s *Store) GetTerminalReportOutcome(ctx context.Context, id domain.SessionID) (domain.ReportTerminalOutcome, bool, error) {
	outcome, err := s.qr.GetTerminalReportOutcome(ctx, string(id))
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get terminal report outcome for %s: %w", id, err)
	}
	return domain.ReportTerminalOutcome(outcome), true, nil
}

// GetTerminalReport returns the first report that durably judged a session.
func (s *Store) GetTerminalReport(ctx context.Context, id domain.SessionID) (domain.ReportRecord, bool, error) {
	var reportID string
	err := s.readDB.QueryRowContext(ctx, `SELECT report_id FROM report_terminal_completions WHERE session_id = ?`, string(id)).Scan(&reportID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ReportRecord{}, false, nil
	}
	if err != nil {
		return domain.ReportRecord{}, false, fmt.Errorf("get terminal report for %s: %w", id, err)
	}
	return s.GetReport(ctx, reportID)
}

func (s *Store) listRemoteReportsByProject(ctx context.Context, id domain.ProjectID) ([]domain.ReportRecord, error) {
	rows, err := s.readDB.QueryContext(ctx, `SELECT host_id, remote_session_id, report_id, project_id, source_project_id, state, terminal_outcome, note, message,
		agent, session_name, host_label, created_at, repeat_count
		FROM remote_worker_reports WHERE project_id = ? ORDER BY created_at, host_id, remote_session_id, report_id`, string(id))
	if err != nil {
		return nil, fmt.Errorf("list remote project reports: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var reports []domain.ReportRecord
	for rows.Next() {
		var report domain.ReportRecord
		var sessionID, projectID, sourceProjectID, state, terminalOutcome string
		if err := rows.Scan(&report.HostID, &sessionID, &report.ID, &projectID, &sourceProjectID, &state, &terminalOutcome, &report.Note, &report.Message,
			&report.Agent, &report.SessionDisplayName, &report.HostLabel, &report.CreatedAt, &report.RepeatCount); err != nil {
			return nil, fmt.Errorf("scan remote project report: %w", err)
		}
		report.SessionID = domain.SessionID(sessionID)
		report.ProjectID = domain.ProjectID(projectID)
		report.SourceProjectID = sourceProjectID
		report.State = domain.ReportState(state)
		report.TerminalOutcome = domain.ReportTerminalOutcome(terminalOutcome)
		report.Outputs, err = s.listRemoteReportOutputs(ctx, report.HostID, sessionID, report.ID)
		if err != nil {
			return nil, err
		}
		reports = append(reports, report)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read remote project reports: %w", err)
	}
	return reports, nil
}

func (s *Store) listRemoteReportOutputs(ctx context.Context, hostID, sessionID, reportID string) (outputs []domain.ReportOutput, err error) {
	rows, err := s.readDB.QueryContext(ctx, `SELECT kind, reference, label FROM remote_worker_report_outputs
		WHERE host_id = ? AND remote_session_id = ? AND report_id = ? ORDER BY position`, hostID, sessionID, reportID)
	if err != nil {
		return nil, fmt.Errorf("list remote report outputs %s: %w", reportID, err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		var output domain.ReportOutput
		var kind string
		if err := rows.Scan(&kind, &output.Reference, &output.Label); err != nil {
			return nil, fmt.Errorf("scan remote report output %s: %w", reportID, err)
		}
		output.Kind = domain.ReportOutputKind(kind)
		outputs = append(outputs, output)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read remote report outputs %s: %w", reportID, err)
	}
	return outputs, nil
}

// ListReportedPRURLs returns only a worker's structured created-PR outputs.
func (s *Store) ListReportedPRURLs(ctx context.Context, id domain.SessionID) ([]string, error) {
	urls, err := s.qr.ListReportedPRURLs(ctx, string(id))
	if err != nil {
		return nil, fmt.Errorf("list reported PR URLs for %s: %w", id, err)
	}
	return urls, nil
}

// ListReportsByProjectPage returns one bounded report page.
// It merges local and remote history with a stable cursor.
func (s *Store) ListReportsByProjectPage(ctx context.Context, id domain.ProjectID, beforeCreatedAt time.Time, beforeHost, beforeSession, beforeID string, limit int) ([]domain.ReportRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	fetchLimit := limit + 1
	query := `SELECT host_id, session_id, report_id, project_id, source_project_id, state, terminal_outcome, note, message,
		agent, session_name, host_label, created_at, repeat_count
		FROM (
			SELECT '' AS host_id, session_id, id AS report_id, project_id, '' AS source_project_id,
				state, terminal_outcome, note, message, '' AS agent, '' AS session_name, '' AS host_label,
				created_at, repeat_count
			FROM reports
			WHERE project_id = ?
			UNION ALL
			SELECT host_id, remote_session_id, report_id, project_id, source_project_id,
				state, terminal_outcome, note, message, agent, session_name, host_label,
				created_at, repeat_count
			FROM remote_worker_reports
			WHERE project_id = ?
		)
		WHERE 1 = 1`
	args := []any{string(id), string(id)}
	if !beforeCreatedAt.IsZero() {
		query += ` AND (created_at < ?
			OR (created_at = ? AND (host_id < ?
				OR (host_id = ? AND (session_id < ?
					OR (session_id = ? AND report_id < ?))))))`
		args = append(args, beforeCreatedAt, beforeCreatedAt, beforeHost, beforeHost, beforeSession, beforeSession, beforeID)
	}
	query += ` ORDER BY created_at DESC, host_id DESC, session_id DESC, report_id DESC LIMIT ?`
	args = append(args, fetchLimit)

	rows, err := s.readDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list project report page: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var reports []domain.ReportRecord
	for rows.Next() {
		var report domain.ReportRecord
		var sessionID, projectID, sourceProjectID, state string
		if err := rows.Scan(&report.HostID, &sessionID, &report.ID, &projectID, &sourceProjectID, &state, &report.TerminalOutcome, &report.Note, &report.Message,
			&report.Agent, &report.SessionDisplayName, &report.HostLabel, &report.CreatedAt, &report.RepeatCount); err != nil {
			return nil, fmt.Errorf("scan project report page: %w", err)
		}
		report.SessionID = domain.SessionID(sessionID)
		report.ProjectID = domain.ProjectID(projectID)
		report.SourceProjectID = sourceProjectID
		report.State = domain.ReportState(state)
		if report.HostID == "" {
			outputs, err := s.reportOutputs(ctx, report.ID)
			if err != nil {
				return nil, err
			}
			report.Outputs = outputs
		} else {
			outputs, err := s.listRemoteReportOutputs(ctx, report.HostID, sessionID, report.ID)
			if err != nil {
				return nil, err
			}
			report.Outputs = outputs
		}
		reports = append(reports, report)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read project report page: %w", err)
	}
	return reports, nil
}

// reportOutputs reads one local report's outputs.
func (s *Store) reportOutputs(ctx context.Context, reportID string) ([]domain.ReportOutput, error) {
	rows, err := s.qr.ListReportOutputs(ctx, reportID)
	if err != nil {
		return nil, fmt.Errorf("list report %s outputs: %w", reportID, err)
	}
	outputs := make([]domain.ReportOutput, len(rows))
	for i, row := range rows {
		outputs[i] = domain.ReportOutput{Kind: domain.ReportOutputKind(row.Kind), Reference: row.Reference, Label: row.Label}
	}
	return outputs, nil
}

// ListReportsByProject is the read-only persisted report projection. Consumers
// such as Project Summary do not participate in the delivery claim lifecycle.
func (s *Store) ListReportsByProject(ctx context.Context, id domain.ProjectID) ([]domain.ReportRecord, error) {
	rows, err := s.qr.ListReportsByProject(ctx, string(id))
	if err != nil {
		return nil, fmt.Errorf("list project reports: %w", err)
	}
	local, err := s.reportsWithOutputs(ctx, rows)
	if err != nil {
		return nil, err
	}
	remote, err := s.listRemoteReportsByProject(ctx, id)
	if err != nil {
		return nil, err
	}
	return append(local, remote...), nil
}

// ListPendingReportSchedule returns the earliest pending work regardless of
// availability so a coordinator can restore the original deadline after restart.
func (s *Store) ListPendingReportSchedule(ctx context.Context, limit int64) ([]domain.ReportRecord, error) {
	rows, err := s.qr.ListPendingReportSchedule(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("list report schedule: %w", err)
	}
	return s.reportsWithOutputs(ctx, rows)
}

// ClaimPendingReportBatch claims one recipient group's pending reports; an
// exitedOwner adds that coordinator's workers to a root group.
func (s *Store) ClaimPendingReportBatch(ctx context.Context, projectID domain.ProjectID, recipientID, rootID domain.SessionID, token string, at time.Time, exitedOwner ...domain.SessionID) ([]domain.ReportRecord, error) {
	if projectID == "" || recipientID == "" || token == "" || at.IsZero() {
		return nil, domain.ErrInvalidReport
	}
	var fallback domain.SessionID
	if len(exitedOwner) > 0 {
		fallback = exitedOwner[0]
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var rows []gen.Report
	err := s.inTx(ctx, "claim recipient report batch", func(q *gen.Queries) error {
		scheduled, err := q.ListPendingReportsByRecipientSchedule(ctx, gen.ListPendingReportsByRecipientScheduleParams{
			ProjectID: string(projectID), RootSessionID: nullableSessionID(rootID),
			RecipientSessionID: nullableSessionID(recipientID), FallbackOwnerID: nullableSessionID(fallback), Limit: 1,
		})
		if err != nil || len(scheduled) == 0 {
			return err
		}
		batchID := scheduled[0].DeliveryBatchID
		if batchID == "" {
			batchID = "report-batch:" + scheduled[0].ID
			if err := q.AssignPendingReportsToRecipient(ctx, gen.AssignPendingReportsToRecipientParams{
				DeliveryBatchID: batchID, ProjectID: string(projectID),
				RootSessionID: nullableSessionID(rootID), RecipientSessionID: nullableSessionID(recipientID),
				FallbackOwnerID: nullableSessionID(fallback),
			}); err != nil {
				return err
			}
		}
		rows, err = q.ClaimPendingReportsByRecipient(ctx, gen.ClaimPendingReportsByRecipientParams{
			ProjectID: string(projectID), DeliveryBatchID: batchID,
			RootSessionID: nullableSessionID(rootID), RecipientSessionID: string(recipientID),
			FallbackOwnerID: nullableSessionID(fallback), ClaimToken: token, ClaimedAt: nullTime(at),
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("claim recipient report batch: %w", err)
	}
	return s.reportsWithOutputs(ctx, orderedReportRows(rows))
}

// AcknowledgePendingSessionReports acknowledges the named reports of one
// worker that are still pending, on behalf of a peer daemon that pulled and
// delivered them. Reports of other sessions, and reports this daemon already
// claimed for its own orchestrator, are left untouched. It returns the IDs it
// acknowledged.
func (s *Store) AcknowledgePendingSessionReports(ctx context.Context, sessionID domain.SessionID, ids []string, token string, at time.Time) ([]string, error) {
	if sessionID == "" || token == "" || at.IsZero() {
		return nil, domain.ErrInvalidReport
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var acknowledged []string
	err := s.inTx(ctx, "acknowledge pulled reports", func(q *gen.Queries) error {
		for _, id := range ids {
			n, err := q.AcknowledgePendingSessionReport(ctx, gen.AcknowledgePendingSessionReportParams{
				ClaimToken: token, AcknowledgedAt: nullTime(at), ID: id, SessionID: string(sessionID),
			})
			if err != nil {
				return err
			}
			if n > 0 {
				acknowledged = append(acknowledged, id)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("acknowledge pulled reports for %s: %w", sessionID, err)
	}
	return acknowledged, nil
}

// AcknowledgeReportBatch completes only the project batch holding token.
func (s *Store) AcknowledgeReportBatch(ctx context.Context, projectID domain.ProjectID, token string, at time.Time) (int, error) {
	if projectID == "" || token == "" || at.IsZero() {
		return 0, domain.ErrInvalidReport
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.AcknowledgeReportBatch(ctx, gen.AcknowledgeReportBatchParams{
		ProjectID: string(projectID), ClaimToken: token, AcknowledgedAt: nullTime(at),
	})
	if err != nil {
		return 0, fmt.Errorf("acknowledge project report batch: %w", err)
	}
	return len(rows), nil
}

// ReleaseReportBatch returns the matching project batch to pending without
// moving any report's original deadline later.
func (s *Store) ReleaseReportBatch(ctx context.Context, projectID domain.ProjectID, token string, at time.Time, lastError string) (int, error) {
	if projectID == "" || token == "" || at.IsZero() {
		return 0, domain.ErrInvalidReport
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.ReleaseReportBatch(ctx, gen.ReleaseReportBatchParams{
		ProjectID: string(projectID), ClaimToken: token, AvailableAt: at, LastError: lastError,
	})
	if err != nil {
		return 0, fmt.Errorf("release project report batch: %w", err)
	}
	return len(rows), nil
}

// DeferReportBatch coalesces a throttled urgent retry until it may request the
// next interruption. Unlike failure release, this intentionally moves its
// delivery availability later.
func (s *Store) DeferReportBatch(ctx context.Context, projectID domain.ProjectID, token string, at time.Time, lastError string) (int, error) {
	if projectID == "" || token == "" || at.IsZero() {
		return 0, domain.ErrInvalidReport
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.DeferReportBatch(ctx, gen.DeferReportBatchParams{
		ProjectID: string(projectID), ClaimToken: token, AvailableAt: at, LastError: lastError,
	})
	if err != nil {
		return 0, fmt.Errorf("defer project report batch: %w", err)
	}
	return len(rows), nil
}

// AcquireReportInterrupt durably enforces the per-worker interruption window.
func (s *Store) AcquireReportInterrupt(ctx context.Context, sessionID domain.SessionID, at time.Time, window time.Duration) (bool, time.Time, error) {
	if sessionID == "" || at.IsZero() || window <= 0 {
		return false, time.Time{}, domain.ErrInvalidReport
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	acquired := false
	next := at
	err := s.inTx(ctx, "acquire report interrupt", func(q *gen.Queries) error {
		last, err := q.GetReportInterrupt(ctx, string(sessionID))
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && last.After(at.Add(-window)) {
			next = last.Add(window)
			return nil
		}
		if err := q.PutReportInterrupt(ctx, gen.PutReportInterruptParams{SessionID: string(sessionID), LastInterruptedAt: at}); err != nil {
			return err
		}
		acquired = true
		return nil
	})
	return acquired, next, err
}

// ReleaseReportInterrupt frees the slot taken at `at` when its interrupt was
// not delivered. Only that exact row goes; absent rows allow interrupts.
func (s *Store) ReleaseReportInterrupt(ctx context.Context, sessionID domain.SessionID, at time.Time) error {
	if sessionID == "" || at.IsZero() {
		return domain.ErrInvalidReport
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.inTx(ctx, "release report interrupt", func(q *gen.Queries) error {
		return q.DeleteReportInterrupt(ctx, gen.DeleteReportInterruptParams{SessionID: string(sessionID), LastInterruptedAt: at})
	})
}

// ListPendingReports returns reports eligible for a later delivery batch.
func (s *Store) ListPendingReports(ctx context.Context, at time.Time, limit int64) ([]domain.ReportRecord, error) {
	rows, err := s.qr.ListPendingReports(ctx, gen.ListPendingReportsParams{AvailableAt: at, Limit: limit})
	if err != nil {
		return nil, fmt.Errorf("list pending reports: %w", err)
	}
	return s.reportsWithOutputs(ctx, rows)
}

// ClaimReport atomically fences one pending report to a delivery attempt token.
func (s *Store) ClaimReport(ctx context.Context, id, token string, at time.Time) (domain.ReportRecord, bool, error) {
	return s.updateReportClaim(ctx, id, token, at, "claim", func() (gen.Report, error) {
		return s.qw.ClaimReport(ctx, gen.ClaimReportParams{ID: id, ClaimToken: token, ClaimedAt: nullTime(at)})
	})
}

// AcknowledgeReport completes only the delivery attempt holding the current token.
func (s *Store) AcknowledgeReport(ctx context.Context, id, token string, at time.Time) (domain.ReportRecord, bool, error) {
	return s.updateReportClaim(ctx, id, token, at, "acknowledge", func() (gen.Report, error) {
		return s.qw.AcknowledgeReport(ctx, gen.AcknowledgeReportParams{ID: id, ClaimToken: token, AcknowledgedAt: nullTime(at)})
	})
}

// ReleaseReport returns the matching token's claim to pending with retry metadata.
func (s *Store) ReleaseReport(ctx context.Context, id, token string, availableAt time.Time, lastError string) (domain.ReportRecord, bool, error) {
	return s.updateReportClaim(ctx, id, token, availableAt, "release", func() (gen.Report, error) {
		return s.qw.ReleaseReport(ctx, gen.ReleaseReportParams{ID: id, ClaimToken: token, AvailableAt: availableAt, LastError: lastError})
	})
}

func (s *Store) updateReportClaim(ctx context.Context, id, token string, at time.Time, action string, update func() (gen.Report, error)) (domain.ReportRecord, bool, error) {
	if id == "" || token == "" || at.IsZero() {
		return domain.ReportRecord{}, false, domain.ErrInvalidReport
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	row, err := update()
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ReportRecord{}, false, nil
	}
	if err != nil {
		return domain.ReportRecord{}, false, fmt.Errorf("%s report %s: %w", action, id, err)
	}
	rec, err := s.reportWithOutputs(ctx, row)
	return rec, err == nil, err
}

// RequeueClaimedReports atomically recovers claims owned by a prior daemon process.
func (s *Store) RequeueClaimedReports(ctx context.Context) (int64, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	count, err := s.qw.RequeueClaimedReports(ctx)
	if err != nil {
		return 0, fmt.Errorf("requeue claimed reports: %w", err)
	}
	return count, nil
}

func (s *Store) reportWithOutputs(ctx context.Context, row gen.Report) (domain.ReportRecord, error) {
	outputs, err := s.qr.ListReportOutputs(ctx, row.ID)
	if err != nil {
		return domain.ReportRecord{}, fmt.Errorf("list report %s outputs: %w", row.ID, err)
	}
	rec := reportFromGen(row)
	rec.Outputs = make([]domain.ReportOutput, len(outputs))
	for i, output := range outputs {
		rec.Outputs[i] = domain.ReportOutput{Kind: domain.ReportOutputKind(output.Kind), Reference: output.Reference, Label: output.Label}
	}
	return rec, nil
}

func (s *Store) reportsWithOutputs(ctx context.Context, rows []gen.Report) ([]domain.ReportRecord, error) {
	out := make([]domain.ReportRecord, 0, len(rows))
	for _, row := range rows {
		rec, err := s.reportWithOutputs(ctx, row)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

func orderedReportRows(rows []gen.Report) []gen.Report {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].ID < rows[j].ID
		}
		return rows[i].CreatedAt.Before(rows[j].CreatedAt)
	})
	return rows
}

func reportFromGen(r gen.Report) domain.ReportRecord {
	return domain.ReportRecord{
		ID: r.ID, SessionID: domain.SessionID(r.SessionID), ProjectID: domain.ProjectID(r.ProjectID),
		State: domain.ReportState(r.State), TerminalOutcome: domain.ReportTerminalOutcome(r.TerminalOutcome), Note: r.Note, Message: r.Message, CreatedAt: r.CreatedAt,
		DeliveryState: domain.ReportDeliveryState(r.DeliveryState), AvailableAt: r.AvailableAt,
		SettlementDeadline: timeFromNull(r.SettlementDeadline), RepeatCount: r.RepeatCount,
		ClaimToken: r.ClaimToken, ClaimedAt: timeFromNull(r.ClaimedAt), DeliveryAttempts: r.DeliveryAttempts,
		AcknowledgedAt: timeFromNull(r.AcknowledgedAt), LastError: r.LastError,
		DeliveryBatchID: r.DeliveryBatchID, DeliveryOwnerID: r.DeliveryOwnerID,
	}
}

// nullableSessionID keeps an empty root ID as SQL NULL so COALESCE can anchor
// NULL owners without treating them as a real session.
func nullableSessionID(id domain.SessionID) *domain.SessionID {
	if id == "" {
		return nil
	}
	return &id
}
