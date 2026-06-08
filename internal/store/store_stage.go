package store

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/domain"
)

func (s *PostgresStore) ListStageParticipants(stageID, limit, offset int) ([]domain.StageParticipant, error) {
	rows, err := s.db.Query(`
				SELECT
					sp.id,
					sp.stage_id,
					sp.linked_client_id,
					sp.linked_lead_id,
					COALESCE(c.name, l.name, sp.participant_name),
					COALESCE(c.email, l.email, sp.participant_email),
					COALESCE(c.phone, l.phone, sp.participant_phone),
					sp.attended,
					sp.created_at
				FROM stage_participants sp
				LEFT JOIN clients c ON c.id = sp.linked_client_id
				LEFT JOIN leads   l ON l.id = sp.linked_lead_id
				WHERE sp.stage_id = $1
				ORDER BY sp.id
				LIMIT $2 OFFSET $3
		`, stageID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.StageParticipant
	for rows.Next() {
		var p domain.StageParticipant
		var nb sql.NullBool
		var createdAt sql.NullTime
		if err := rows.Scan(
			&p.ID,
			&p.StageID,
			&p.LinkedClientID,
			&p.LinkedLeadID,
			&p.ParticipantName,
			&p.ParticipantEmail,
			&p.ParticipantPhone,
			&nb,
			&createdAt,
		); err != nil {
			return nil, err
		}
		if nb.Valid {
			b := nb.Bool
			p.Attended = &b
		} else {
			p.Attended = nil
		}
		if createdAt.Valid {
			s := createdAt.Time.Format(time.RFC3339)
			p.CreatedAt = &s
		} else {
			p.CreatedAt = nil
		}
		out = append(out, p)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if out == nil {
		out = []domain.StageParticipant{}
	}
	return out, nil
}

func (s *PostgresStore) ListStages() ([]domain.Stage, error) {
	rows, err := s.db.Query(`
SELECT
  s.id,
  s.name,
  s.date,
  s.ad_budget,
  s.registrations,
  s.participants,
	COUNT(sp.id) AS recorded_contacts,
	COALESCE(cm.closed_contracts, 0) AS closed_contracts,
	COALESCE(cm.actual_revenue, 0) AS actual_revenue
FROM stages s
LEFT JOIN stage_participants sp ON sp.stage_id = s.id
LEFT JOIN (
	SELECT
		sp.stage_id,
		COUNT(DISTINCT c.id) AS closed_contracts,
		COALESCE(SUM(c.revenue_total), 0) AS actual_revenue
	FROM sales_process sp
	JOIN contracts c ON c.sales_process_id = sp.id
	WHERE sp.stage_id IS NOT NULL
	  AND (sp.stage = 'closed' OR COALESCE(sp.closed, false) = true)
	GROUP BY sp.stage_id
) cm ON cm.stage_id = s.id
GROUP BY
  s.id,
  s.name,
  s.date,
  s.ad_budget,
  s.registrations,
	 s.participants,
	 cm.closed_contracts,
	 cm.actual_revenue
ORDER BY s.id;
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stages []domain.Stage
	for rows.Next() {
		var s domain.Stage
		var closedContracts int
		var actualRevenue float64
		if err := rows.Scan(
			&s.ID,
			&s.Name,
			&s.Date,
			&s.AdBudget,
			&s.Registrations,
			&s.Participants,
			&s.RecordedContacts,
			&closedContracts,
			&actualRevenue,
		); err != nil {
			return nil, err
		}

		s.ClosedContracts = &closedContracts
		s.ActualRevenue = &actualRevenue

		if s.Registrations != nil && *s.Registrations > 0 && s.Participants != nil {
			attendanceRate := roundFloat((float64(*s.Participants)/float64(*s.Registrations))*100, 1)
			s.AttendanceRate = &attendanceRate
		}

		if s.Participants != nil && *s.Participants > 0 {
			closingRate := roundFloat((float64(closedContracts)/float64(*s.Participants))*100, 1)
			s.ClosingRate = &closingRate
		}

		if s.AdBudget != nil && *s.AdBudget > 0 {
			roi := roundFloat(actualRevenue / *s.AdBudget, 2)
			s.ROI = &roi
		}

		s.MonetaryMode = monetaryModeBrutto

		stages = append(stages, s)
	}

	if stages == nil {
		stages = []domain.Stage{}
	}
	return stages, rows.Err()
}

func (s *PostgresStore) CreateStage(stage domain.Stage) (domain.Stage, error) {
	var id int
	err := s.db.QueryRow(`
		INSERT INTO stages (name, date, ad_budget, registrations, participants)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, stage.Name, stage.Date, stage.AdBudget, stage.Registrations, stage.Participants).Scan(&id)

	if err != nil {
		return domain.Stage{}, err
	}
	stage.ID = id
	return stage, nil
}

func (s *PostgresStore) AddStageParticipant(stageID int, p domain.StageParticipant) (domain.StageParticipant, error) {
	var id int
	err := s.db.QueryRow(`
		INSERT INTO stage_participants (
			stage_id, participant_name, participant_email, participant_phone,
			linked_client_id, linked_lead_id, attended
		) VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id
	`,
		stageID,
		p.ParticipantName,
		p.ParticipantEmail,
		p.ParticipantPhone,
		p.LinkedClientID,
		p.LinkedLeadID,
		p.Attended,
	).Scan(&id)
	if err != nil {
		return domain.StageParticipant{}, err
	}
	p.ID = id
	p.StageID = stageID
	return p, nil
}

func (s *PostgresStore) InsertLeadForStage(name, email, phone string, stageID int) (int, error) {
	var id int
	err := s.db.QueryRow(`
		INSERT INTO leads (name, email, phone, source, source_stage_id)
		VALUES ($1, $2, $3, 'paid', $4)
		RETURNING id
	`, name, email, phone, stageID).Scan(&id)
	return id, err
}

func (s *PostgresStore) UpdateStageParticipant(stageID int, request domain.StageParticipant) error {

	_, err := s.db.Exec(`
		UPDATE stage_participants
		SET attended = COALESCE($1, attended)
		WHERE id = $2 AND stage_id = $3`,
		request.Attended, request.ID, stageID,
	)
	if err != nil {
		return err
	}

	return nil
}

func (s *PostgresStore) DeleteStage(stageID int) error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM stage_participants WHERE stage_id = $1`, stageID); err != nil {
		return err
	}

	if _, err := tx.Exec(`DELETE FROM stage_client_assignments WHERE stage_id = $1`, stageID); err != nil {
		return err
	}

	res, err := tx.Exec(`DELETE FROM stages WHERE id = $1`, stageID)
	if err != nil {
		return err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("stage not found")
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

// func (s *PostgresStore) AddStageParticipant(stageID int, participant domain.StageParticipant) (*domain.StageParticipant, error) {

// }

func (s *PostgresStore) DeleteStageParticipant(stageID, participantID int) error {
	res, err := s.db.Exec(`
		DELETE FROM stage_participants
		WHERE id = $1 AND stage_id = $2
	`, participantID, stageID)

	if err != nil {
		return err
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("participant not found")
	}
	return nil

}

func (s *PostgresStore) AssignClientToStage(stageID, clientID int) error {
	_, err := s.db.Exec(
		`INSERT INTO stage_client_assignments (client_id, stage_id) VALUES ($1, $2)`,
		clientID, stageID,
	)
	if err != nil {
		return err
	}
	return nil
}

func (s *PostgresStore) UpdateStageInfo(stageID int, name, date *string, adBudget *float64) error {
	_, err := s.db.Exec(`
		UPDATE stages
		SET
			name = COALESCE($1, name),
			date = COALESCE($2, date),
			ad_budget = COALESCE($3, ad_budget)
		WHERE id = $4
	`, name, date, adBudget, stageID)
	return err
}

func (s *PostgresStore) UpdateStageStats(stageID int, registrations *int, participants *int) error {
	_, err := s.db.Exec(`
		UPDATE stages
		SET registrations = COALESCE($1, registrations),
		    participants = COALESCE($2, participants)
		WHERE id = $3
	`, registrations, participants, stageID)

	if err != nil {
		return err
	}
	return nil
}

func roundFloat(value float64, decimals int) float64 {
	pow := math.Pow(10, float64(decimals))
	return math.Round(value*pow) / pow
}
