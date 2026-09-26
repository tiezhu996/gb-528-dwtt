package model

import (
	"time"

	"gorm.io/datatypes"
)

type RehearsalRun struct {
	ID                   uint           `gorm:"primaryKey"`
	CueSetVersion        string         `gorm:"size:160;not null;index"`
	RunStatus            string         `gorm:"size:32;not null;check:chk_run_status,run_status IN ('evaluated','blocked','pending_review','approved_for_rehearsal','rejected')"`
	TimelineSnapshotJSON datatypes.JSON `gorm:"type:jsonb;not null"`
	RuleResultsJSON      datatypes.JSON `gorm:"type:jsonb;not null"`
	CollisionWindowsJSON datatypes.JSON `gorm:"type:jsonb;not null"`
	HighestSeverity      string         `gorm:"size:16;not null;check:chk_run_severity,highest_severity IN ('pass','warning','blocker','invalid')"`
	StartedBy            uint           `gorm:"not null"`
	ReviewedBy           *uint
	ReviewReason         string    `gorm:"size:500"`
	Version              uint      `gorm:"not null;default:1"`
	FinishedAt           time.Time `gorm:"not null"`
	ReviewedAt           *time.Time
	CreatedAt            time.Time
}

func (RehearsalRun) TableName() string { return "rehearsal_runs" }

// BlockerDisposition is one safety reviewer's decision on a single piece of
// blocking evidence (result blocker or invalid) of a submitted rehearsal run.
// The evidence itself stays immutable inside rehearsal_runs.rule_results_json;
// this row records who reviewed the blocker, when, the decision and why.
type BlockerDisposition struct {
	ID           uint      `gorm:"primaryKey"`
	RunID        uint      `gorm:"not null;index:idx_blocker_disposition_run;uniqueIndex:uk_blocker_disposition_key,priority:1"`
	EvidenceKey  string    `gorm:"size:64;not null;uniqueIndex:uk_blocker_disposition_key,priority:2"`
	RuleCode     string    `gorm:"size:64;not null;index:idx_blocker_disposition_rule"`
	Decision     string    `gorm:"size:32;not null;check:chk_blocker_disposition_decision,decision IN ('accepted','needs_rectification')"`
	Reason       string    `gorm:"size:500;not null"`
	ReviewedBy   uint      `gorm:"not null"`
	ReviewerName string    `gorm:"size:64;not null"`
	RegisteredAt time.Time `gorm:"not null"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (BlockerDisposition) TableName() string { return "blocker_dispositions" }
