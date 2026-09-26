package repository

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"stage-rigging-cue-interlock/backend/internal/audit"
	"stage-rigging-cue-interlock/backend/internal/constants"
	"stage-rigging-cue-interlock/backend/internal/model"
	"stage-rigging-cue-interlock/backend/internal/util"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RehearsalRunRepository struct {
	db    *gorm.DB
	audit *audit.Repository
}

func NewRehearsalRunRepository(db *gorm.DB, auditRepository *audit.Repository) *RehearsalRunRepository {
	return &RehearsalRunRepository{db: db, audit: auditRepository}
}

func (r *RehearsalRunRepository) List(page, pageSize int, status, severity, search string) ([]model.RehearsalRun, int64, error) {
	query := r.db.Model(&model.RehearsalRun{})
	if status != "" {
		query = query.Where("run_status = ?", status)
	}
	if severity != "" {
		query = query.Where("highest_severity = ?", severity)
	}
	if search != "" {
		query = query.Where("LOWER(cue_set_version) LIKE ?", "%"+strings.ToLower(search)+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count rehearsal runs: %w", err)
	}
	var items []model.RehearsalRun
	if err := query.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list rehearsal runs: %w", err)
	}
	return items, total, nil
}

func (r *RehearsalRunRepository) Get(id uint) (model.RehearsalRun, error) {
	var item model.RehearsalRun
	if err := r.db.First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.RehearsalRun{}, util.NotFound("RUN_NOT_FOUND", "rehearsal run was not found")
		}
		return model.RehearsalRun{}, fmt.Errorf("get rehearsal run: %w", err)
	}
	return item, nil
}

func (r *RehearsalRunRepository) Create(item *model.RehearsalRun, event audit.Event) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(item).Error; err != nil {
			return fmt.Errorf("create immutable rehearsal run: %w", err)
		}
		event.EntityID = item.ID
		return r.audit.WithTx(tx).Record(event)
	})
}

func (r *RehearsalRunRepository) Transition(id, expectedVersion uint, from, to constants.RehearsalStatus, reviewerID *uint, reason string, event audit.Event) (model.RehearsalRun, error) {
	var updated model.RehearsalRun
	err := r.db.Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{"run_status": to, "version": expectedVersion + 1, "review_reason": reason}
		if reviewerID != nil {
			now := time.Now().UTC()
			updates["reviewed_by"] = reviewerID
			updates["reviewed_at"] = now
		}
		result := tx.Model(&model.RehearsalRun{}).Where("id = ? AND version = ? AND run_status = ?", id, expectedVersion, from).Updates(updates)
		if result.Error != nil {
			return fmt.Errorf("transition rehearsal run: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return util.Conflict("RUN_VERSION_CONFLICT", "rehearsal run state or version changed concurrently", nil)
		}
		if err := r.audit.WithTx(tx).Record(event); err != nil {
			return err
		}
		if err := tx.First(&updated, id).Error; err != nil {
			return fmt.Errorf("reload rehearsal run: %w", err)
		}
		return nil
	})
	return updated, err
}

type BlockerDispositionRepository struct {
	db    *gorm.DB
	audit *audit.Repository
}

func NewBlockerDispositionRepository(db *gorm.DB, auditRepository *audit.Repository) *BlockerDispositionRepository {
	return &BlockerDispositionRepository{db: db, audit: auditRepository}
}

// ListByRun returns every registered disposition for one run, oldest first.
func (r *BlockerDispositionRepository) ListByRun(runID uint) ([]model.BlockerDisposition, error) {
	var items []model.BlockerDisposition
	if err := r.db.Where("run_id = ?", runID).Order("id ASC").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list blocker dispositions: %w", err)
	}
	return items, nil
}

// ListByRuns batches dispositions for run listings, keyed by run id.
func (r *BlockerDispositionRepository) ListByRuns(runIDs []uint) (map[uint][]model.BlockerDisposition, error) {
	grouped := make(map[uint][]model.BlockerDisposition, len(runIDs))
	if len(runIDs) == 0 {
		return grouped, nil
	}
	var items []model.BlockerDisposition
	if err := r.db.Where("run_id IN ?", runIDs).Order("id ASC").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list blocker dispositions: %w", err)
	}
	for _, item := range items {
		grouped[item.RunID] = append(grouped[item.RunID], item)
	}
	return grouped, nil
}

// Register upserts one reviewer disposition and bumps the run's optimistic
// lock version atomically. Run status and evidence key are validated by the
// caller; the version guard rejects concurrent decisions.
func (r *BlockerDispositionRepository) Register(runID, expectedVersion uint, disposition model.BlockerDisposition, event audit.Event) (model.RehearsalRun, error) {
	var updated model.RehearsalRun
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var run model.RehearsalRun
		lock := tx
		if tx.Dialector.Name() == "postgres" {
			lock = tx.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := lock.First(&run, runID).Error; err != nil {
			return fmt.Errorf("lock rehearsal run for disposition: %w", err)
		}
		if run.Version != expectedVersion {
			return util.Conflict("RUN_VERSION_CONFLICT", "rehearsal run state or version changed concurrently", nil)
		}
		now := time.Now().UTC()
		disposition.RunID = runID
		disposition.RegisteredAt = now
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "run_id"}, {Name: "evidence_key"}},
			DoUpdates: clause.AssignmentColumns([]string{"decision", "reason", "reviewed_by", "reviewer_name", "registered_at", "updated_at"}),
		}).Create(&disposition).Error; err != nil {
			return fmt.Errorf("register blocker disposition: %w", err)
		}
		result := tx.Model(&model.RehearsalRun{}).
			Where("id = ? AND version = ?", runID, expectedVersion).
			Update("version", expectedVersion+1)
		if result.Error != nil {
			return fmt.Errorf("bump rehearsal run version: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return util.Conflict("RUN_VERSION_CONFLICT", "rehearsal run state or version changed concurrently", nil)
		}
		if err := r.audit.WithTx(tx).Record(event); err != nil {
			return err
		}
		return tx.First(&updated, runID).Error
	})
	return updated, err
}
