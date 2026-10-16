package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func StoreVFXFilesSyncPreview(db *gorm.DB, productionID, snapshotID string, configRevision uint64, preview []byte) error {
	productionID = strings.ToLower(strings.TrimSpace(productionID))
	if db == nil || !vfxFilesUUIDRE.MatchString(productionID) || !vfxFilesUUIDRE.MatchString(strings.TrimSpace(snapshotID)) || configRevision == 0 || len(preview) == 0 || len(preview) > 512<<10 {
		return ErrVFXFilesInvalidBatch
	}
	var value struct {
		ContractVersion string `json:"contract_version"`
		PreviewID       string `json:"preview_id"`
		ProductionID    string `json:"production_id"`
		ConfigRevision  uint64 `json:"config_revision"`
		State           string `json:"state"`
		ExpiresAt       string `json:"expires_at"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(preview)))
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF || value.ContractVersion != "1.0" || !vfxFilesUUIDRE.MatchString(value.PreviewID) || !strings.EqualFold(value.ProductionID, productionID) || value.ConfigRevision != configRevision || value.State != "preview_ready" {
		return ErrVFXFilesInvalidBatch
	}
	expires, err := time.Parse(time.RFC3339, value.ExpiresAt)
	if err != nil || !expires.After(time.Now().UTC()) {
		return ErrVFXFilesInvalidBatch
	}
	row := VFXFilesSyncOperation{ProductionID: productionID, ConfigRevision: configRevision, SnapshotID: strings.ToLower(strings.TrimSpace(snapshotID)), PreviewID: strings.ToLower(value.PreviewID), PreviewJSON: string(preview), PreviewExpiresAt: &expires, JobID: "", JobJSON: "", ApplyIdempotency: "", RetryIdempotency: "", ReconcileIdempotency: "", UpdatedAt: time.Now().UTC()}
	return db.Transaction(func(tx *gorm.DB) error {
		var existing VFXFilesSyncOperation
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("production_id = ?", productionID).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Create(&row).Error
		}
		if err != nil {
			return err
		}
		if existing.JobID != "" || existing.JobJSON != "" {
			var job struct {
				State string `json:"state"`
			}
			if existing.JobID == "" || existing.JobJSON == "" || json.Unmarshal([]byte(existing.JobJSON), &job) != nil || (job.State != "completed" && job.State != "cancelled") {
				return ErrVFXFilesSyncJobUnresolved
			}
		}
		result := tx.Model(&VFXFilesSyncOperation{}).
			Where("id = ? AND job_id = ? AND job_json = ?", existing.ID, existing.JobID, existing.JobJSON).
			Updates(map[string]any{
				"config_revision": row.ConfigRevision, "snapshot_id": row.SnapshotID, "preview_id": row.PreviewID,
				"preview_json": row.PreviewJSON, "preview_expires_at": row.PreviewExpiresAt,
				"job_id": "", "job_json": "", "apply_idempotency": "", "retry_idempotency": "",
				"reconcile_idempotency": "", "updated_at": row.UpdatedAt,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrVFXFilesConfigConflict
		}
		return nil
	})
}

func FindVFXFilesSyncOperation(db *gorm.DB, productionID string) (VFXFilesSyncOperation, error) {
	var row VFXFilesSyncOperation
	if db == nil || !vfxFilesUUIDRE.MatchString(strings.TrimSpace(productionID)) {
		return row, ErrVFXFilesInvalidBatch
	}
	err := db.Where("production_id = ?", strings.ToLower(strings.TrimSpace(productionID))).First(&row).Error
	return row, err
}

func PrepareVFXFilesSyncCommand(db *gorm.DB, productionID, command, idempotency string) (VFXFilesSyncOperation, error) {
	var row VFXFilesSyncOperation
	if db == nil || !vfxFilesUUIDRE.MatchString(strings.TrimSpace(productionID)) || len(idempotency) < 16 || len(idempotency) > 128 {
		return row, ErrVFXFilesInvalidBatch
	}
	column := ""
	switch command {
	case "apply":
		column = "apply_idempotency"
	case "retry":
		column = "retry_idempotency"
	case "reconcile":
		column = "reconcile_idempotency"
	default:
		return row, ErrVFXFilesInvalidBatch
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("production_id = ?", strings.ToLower(strings.TrimSpace(productionID))).First(&row).Error; err != nil {
			return err
		}
		current := ""
		switch command {
		case "apply":
			current = row.ApplyIdempotency
		case "retry":
			current = row.RetryIdempotency
		case "reconcile":
			current = row.ReconcileIdempotency
		}
		if current == "" {
			result := tx.Model(&row).Where("production_id = ? AND "+column+" = ?", row.ProductionID, "").Update(column, idempotency)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrVFXFilesConfigConflict
			}
			switch command {
			case "apply":
				row.ApplyIdempotency = idempotency
			case "retry":
				row.RetryIdempotency = idempotency
			case "reconcile":
				row.ReconcileIdempotency = idempotency
			}
		}
		return nil
	})
	return row, err
}

func StoreVFXFilesSyncJob(db *gorm.DB, productionID string, job []byte) error {
	if db == nil || !vfxFilesUUIDRE.MatchString(strings.TrimSpace(productionID)) || len(job) == 0 || len(job) > 64<<10 {
		return ErrVFXFilesInvalidBatch
	}
	var value struct {
		ContractVersion string `json:"contract_version"`
		ID              string `json:"id"`
		ProductionID    string `json:"production_id"`
		ConfigRevision  uint64 `json:"config_revision"`
		State           string `json:"state"`
		Progress        struct {
			Completed int `json:"completed"`
			Total     int `json:"total"`
		} `json:"progress"`
		UpdatedAt string `json:"updated_at"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(job)))
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF || value.ContractVersion != "1.0" || !vfxFilesUUIDRE.MatchString(value.ID) || !strings.EqualFold(value.ProductionID, productionID) || value.ConfigRevision == 0 || value.Progress.Completed < 0 || value.Progress.Total < value.Progress.Completed {
		return ErrVFXFilesInvalidBatch
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, value.UpdatedAt)
	if err != nil {
		return ErrVFXFilesInvalidBatch
	}
	switch value.State {
	case "queued", "running", "completed", "failed", "reconcile_required", "cancelled":
	default:
		return ErrVFXFilesInvalidBatch
	}
	var previous VFXFilesSyncOperation
	if err := db.Where("production_id = ? AND config_revision = ?", strings.ToLower(productionID), value.ConfigRevision).First(&previous).Error; err != nil {
		return err
	}
	var previousPayload struct {
		ID        string `json:"id"`
		State     string `json:"state"`
		UpdatedAt string `json:"updated_at"`
	}
	var previousUpdatedAt time.Time
	if previous.JobID != "" || previous.JobJSON != "" {
		if previous.JobID == "" || previous.JobJSON == "" || json.Unmarshal([]byte(previous.JobJSON), &previousPayload) != nil || !strings.EqualFold(previousPayload.ID, previous.JobID) || previousPayload.State == "" {
			return ErrVFXFilesConfigConflict
		}
		previousUpdatedAt, err = time.Parse(time.RFC3339Nano, previousPayload.UpdatedAt)
		if err != nil || !strings.EqualFold(previous.JobID, value.ID) || updatedAt.Before(previousUpdatedAt) || updatedAt.Equal(previousUpdatedAt) && value.State != previousPayload.State || previousPayload.State == "completed" && value.State != "completed" || previousPayload.State == "cancelled" && value.State != "cancelled" {
			return ErrVFXFilesConfigConflict
		}
	}
	var nextPayload struct {
		State     string `json:"state"`
		UpdatedAt string `json:"updated_at"`
	}
	_ = json.Unmarshal(job, &nextPayload)
	updates := map[string]any{"job_id": strings.ToLower(value.ID), "job_json": string(job), "updated_at": time.Now().UTC()}
	if previousPayload.State != "" && (previousPayload.State != nextPayload.State || previousPayload.UpdatedAt != nextPayload.UpdatedAt) {
		updates["retry_idempotency"] = ""
		updates["reconcile_idempotency"] = ""
	}
	result := db.Model(&VFXFilesSyncOperation{}).
		Where("production_id = ? AND config_revision = ? AND job_id = ? AND job_json = ?", strings.ToLower(productionID), value.ConfigRevision, previous.JobID, previous.JobJSON).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrVFXFilesConfigConflict
	}
	return nil
}

const (
	VFXFilesEventAccepted    = "accepted"
	VFXFilesEventDuplicate   = "duplicate"
	VFXFilesEventQuarantined = "quarantined"
)

var (
	ErrVFXFilesEventConflict               = errors.New("VFX Files event identity conflict")
	ErrVFXFilesActivationPending           = errors.New("VFX Files activation mirror is pending")
	ErrVFXFilesAuthorityTransition         = errors.New("VFX Files authority transition cannot be proven")
	ErrVFXFilesAuthorityUnresolved         = errors.New("VFX Files old-epoch delivery outcome remains unresolved")
	ErrVFXFilesInvalidBatch                = errors.New("VFX Files event batch is invalid")
	ErrVFXFilesConfigConflict              = errors.New("VFX Files desired config revision conflict")
	ErrVFXFilesSyncJobUnresolved           = errors.New("VFX Files sync job is unresolved")
	ErrVFXFilesAuthorizationIntentConflict = errors.New("VFX Files authorization intent identity conflict")
)

var vfxFilesUUIDRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

type VFXFilesDesiredConfigMirror struct {
	ContractVersion string `json:"contract_version"`
	ProductionID    string `json:"production_id"`
	ConfigRevision  uint64 `json:"config_revision"`
	ConnectionID    string `json:"connection_id"`
	Drive           struct {
		AccountMode string  `json:"account_mode"`
		DriveID     *string `json:"drive_id,omitempty"`
		Owner       *string `json:"owner_subject,omitempty"`
		RootID      string  `json:"root_id"`
		RootState   string  `json:"root_state"`
	} `json:"drive"`
	TemplateSelection struct {
		Selections []struct {
			Scope     string            `json:"scope"`
			ID        string            `json:"template_id"`
			Revision  uint64            `json:"revision"`
			Overrides map[string]string `json:"overrides"`
		} `json:"selections"`
	} `json:"template_selection"`
	SyncPolicy struct {
		Mode  string `json:"mode"`
		Scope struct {
			Episodes    bool     `json:"episodes"`
			Sequences   bool     `json:"sequences"`
			Shots       bool     `json:"shots"`
			Assets      bool     `json:"assets"`
			Tasks       bool     `json:"tasks"`
			TaskTypeIDs []string `json:"task_type_ids,omitempty"`
			IncludeIDs  []string `json:"include_ids,omitempty"`
			ExcludeIDs  []string `json:"exclude_ids,omitempty"`
		} `json:"scope"`
		NotifyNewDeliveries bool `json:"notify_new_deliveries"`
	} `json:"sync_policy"`
	Activation struct {
		Enabled           bool    `json:"enabled"`
		ActivatedAt       *string `json:"activated_at"`
		NotificationEpoch uint64  `json:"notification_epoch"`
	} `json:"activation"`
}

// StoreVFXFilesDesiredConfigMirror persists only a validated, non-secret
// Plugin response. Revision equality is idempotent only when its exact body
// matches; older responses cannot roll local event eligibility backward.
func StoreVFXFilesDesiredConfigMirror(db *gorm.DB, productionID string, body []byte) error {
	productionID = strings.ToLower(strings.TrimSpace(productionID))
	if db == nil || !vfxFilesUUIDRE.MatchString(productionID) || len(body) == 0 || len(body) > 256<<10 {
		return ErrVFXFilesInvalidBatch
	}
	var config VFXFilesDesiredConfigMirror
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&config) != nil || decoder.Decode(new(any)) != io.EOF {
		return ErrVFXFilesInvalidBatch
	}
	if config.ContractVersion != "1.0" || !strings.EqualFold(config.ProductionID, productionID) || config.ConfigRevision == 0 || !vfxFilesUUIDRE.MatchString(config.ConnectionID) || config.Activation.Enabled && config.Activation.NotificationEpoch == 0 || config.Drive.RootState != "verified" || !driveObjectIDRE.MatchString(config.Drive.RootID) || config.Drive.AccountMode != "my_drive" && config.Drive.AccountMode != "shared_drive" {
		return ErrVFXFilesInvalidBatch
	}
	if config.Drive.AccountMode == "shared_drive" && (config.Drive.DriveID == nil || !driveObjectIDRE.MatchString(*config.Drive.DriveID)) || config.Drive.AccountMode == "my_drive" && (config.Drive.Owner == nil || !boundedVFXText(*config.Drive.Owner, 240)) {
		return ErrVFXFilesInvalidBatch
	}
	if len(config.TemplateSelection.Selections) == 0 || config.SyncPolicy.Mode != "new_production" && config.SyncPolicy.Mode != "adopt_existing" && config.SyncPolicy.Mode != "future_only" || !validVFXIDList(config.SyncPolicy.Scope.TaskTypeIDs, 500) || !validVFXIDList(config.SyncPolicy.Scope.IncludeIDs, 500000) || !validVFXIDList(config.SyncPolicy.Scope.ExcludeIDs, 500000) {
		return ErrVFXFilesInvalidBatch
	}
	seenScopes := map[string]bool{}
	for _, selection := range config.TemplateSelection.Selections {
		if !validVFXTemplateScope(selection.Scope) || seenScopes[selection.Scope] || !boundedVFXText(selection.ID, 160) || selection.Revision == 0 || len(selection.Overrides) > 100 {
			return ErrVFXFilesInvalidBatch
		}
		seenScopes[selection.Scope] = true
		for key, value := range selection.Overrides {
			if !boundedVFXText(key, 100) || len([]rune(value)) > 240 {
				return ErrVFXFilesInvalidBatch
			}
		}
	}
	if config.Activation.Enabled && (config.Activation.ActivatedAt == nil || !boundedVFXText(*config.Activation.ActivatedAt, 50)) {
		return ErrVFXFilesInvalidBatch
	}
	canonicalBody, err := canonicalVFXFilesJSON(body)
	if err != nil {
		return ErrVFXFilesInvalidBatch
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		var existing VFXFilesProductionConfig
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("production_id = ?", productionID).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			row := VFXFilesProductionConfig{ProductionID: productionID, DesiredConfigJSON: string(canonicalBody), ConfigRevision: config.ConfigRevision, ActivationEpoch: config.Activation.NotificationEpoch, Activated: config.Activation.Enabled, UpdatedAt: time.Now().UTC()}
			return tx.Create(&row).Error
		}
		if err != nil {
			return err
		}
		existingCanonical, err := canonicalVFXFilesJSON([]byte(existing.DesiredConfigJSON))
		if err != nil {
			return ErrVFXFilesInvalidBatch
		}
		if config.Activation.NotificationEpoch < existing.ActivationEpoch || config.ConfigRevision < existing.ConfigRevision || config.ConfigRevision == existing.ConfigRevision && (!bytes.Equal(existingCanonical, canonicalBody) || existing.ActivationEpoch != config.Activation.NotificationEpoch || existing.Activated != config.Activation.Enabled) {
			return ErrVFXFilesConfigConflict
		}
		updates := map[string]any{
			"desired_config_json": string(canonicalBody), "config_revision": config.ConfigRevision, "activation_epoch": config.Activation.NotificationEpoch,
			"activated": config.Activation.Enabled, "updated_at": time.Now().UTC(),
		}
		if existing.PendingActivationEpoch != 0 && config.Activation.NotificationEpoch >= existing.PendingActivationEpoch {
			updates["pending_activation_epoch"] = 0
		}
		result := tx.Model(&VFXFilesProductionConfig{}).Where("id = ? AND config_revision <= ?", existing.ID, config.ConfigRevision).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrVFXFilesConfigConflict
		}
		return nil
	})
	return err
}

func canonicalVFXFilesJSON(body []byte) ([]byte, error) {
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, ErrVFXFilesInvalidBatch
	}
	return json.Marshal(value)
}

// DisableVFXFilesEventIntake pauses local event acceptance and delivery before
// an external deactivation request. If that request has an unknown outcome,
// KitsuSync remains fail-closed until the desired-config mirror is reconciled.
func DisableVFXFilesEventIntake(db *gorm.DB, productionID string) error {
	productionID = strings.ToLower(strings.TrimSpace(productionID))
	if db == nil || !vfxFilesUUIDRE.MatchString(productionID) {
		return ErrVFXFilesInvalidBatch
	}
	result := db.Model(&VFXFilesProductionConfig{}).Where("production_id = ?", productionID).Updates(map[string]any{"activated": false, "pending_activation_epoch": 0})
	return result.Error
}

var driveObjectIDRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)

func boundedVFXText(value string, max int) bool {
	value = strings.TrimSpace(value)
	return value != "" && len([]rune(value)) <= max && !strings.ContainsAny(value, "\x00\r\n")
}

func validVFXTemplateScope(value string) bool {
	switch value {
	case "production", "asset", "sequence", "shot", "task":
		return true
	default:
		return false
	}
}

func validVFXIDList(values []string, limit int) bool {
	if len(values) > limit {
		return false
	}
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if !vfxFilesUUIDRE.MatchString(value) || seen[strings.ToLower(value)] {
			return false
		}
		seen[strings.ToLower(value)] = true
	}
	return true
}

// VFXFilesProductionConfig stores a validated, non-secret mirror of the
// Plugin-owned Production Drive authority/configuration for event admission
// and delivery fencing. Existing rows also remain the explicit legacy-import
// source until an administrator completes the Plugin import. KitsuSync is not
// an authority writer, and this mirror never contains OAuth credentials or a
// Plugin credential reference.
type VFXFilesProductionConfig struct {
	ID                     uint   `gorm:"primaryKey"`
	ProductionID           string `gorm:"not null;uniqueIndex;size:64"`
	DesiredConfigJSON      string `gorm:"type:text;not null;default:''"`
	ConfigRevision         uint64 `gorm:"not null;default:0"`
	ActivationEpoch        uint64 `gorm:"not null;default:0"`
	PendingActivationEpoch uint64 `gorm:"not null;default:0"`
	Activated              bool   `gorm:"not null;default:false;index"`
	UpdatedAt              time.Time
}

func (VFXFilesProductionConfig) TableName() string { return "vfx_files_production_configs" }

// VFXFilesAuthorizationIntent binds the Plugin-owned OAuth return to the
// initiating KitsuSync actor and Production. It stores only opaque identity
// and expiry metadata; OAuth codes and tokens never pass through this table.
type VFXFilesAuthorizationIntent struct {
	ID              uint      `gorm:"primaryKey"`
	AuthorizationID string    `gorm:"not null;uniqueIndex;size:64"`
	ProductionID    string    `gorm:"not null;index;size:64"`
	ActorID         string    `gorm:"not null;size:64"`
	ExpiresAt       time.Time `gorm:"not null;index"`
	ConsumedAt      *time.Time
	CreatedAt       time.Time
}

func (VFXFilesAuthorizationIntent) TableName() string { return "vfx_files_authorization_intents" }

// StoreVFXFilesAuthorizationIntent is idempotent for the same intent and
// refuses to rebind an opaque authorization ID to another actor or Production.
func StoreVFXFilesAuthorizationIntent(db *gorm.DB, intent VFXFilesAuthorizationIntent) error {
	if db == nil || !vfxFilesUUIDRE.MatchString(strings.TrimSpace(intent.AuthorizationID)) || !vfxFilesUUIDRE.MatchString(strings.TrimSpace(intent.ProductionID)) || !vfxFilesUUIDRE.MatchString(strings.TrimSpace(intent.ActorID)) || !intent.ExpiresAt.After(time.Now().UTC()) {
		return ErrVFXFilesInvalidBatch
	}
	intent.AuthorizationID = strings.ToLower(strings.TrimSpace(intent.AuthorizationID))
	intent.ProductionID = strings.ToLower(strings.TrimSpace(intent.ProductionID))
	intent.ActorID = strings.ToLower(strings.TrimSpace(intent.ActorID))
	if intent.CreatedAt.IsZero() {
		intent.CreatedAt = time.Now().UTC()
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var expiredIDs []uint
		if err := tx.Model(&VFXFilesAuthorizationIntent{}).Where("expires_at <= ?", time.Now().UTC()).Order("id").Limit(100).Pluck("id", &expiredIDs).Error; err != nil {
			return err
		}
		if len(expiredIDs) > 0 {
			if err := tx.Where("id IN ?", expiredIDs).Delete(&VFXFilesAuthorizationIntent{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "authorization_id"}}, DoNothing: true}).Create(&intent).Error; err != nil {
			return err
		}
		var stored VFXFilesAuthorizationIntent
		if err := tx.Where("authorization_id = ?", intent.AuthorizationID).Take(&stored).Error; err != nil {
			return err
		}
		if stored.ProductionID != intent.ProductionID || stored.ActorID != intent.ActorID || !stored.ExpiresAt.Equal(intent.ExpiresAt) {
			return ErrVFXFilesAuthorizationIntentConflict
		}
		return nil
	})
}

func FindVFXFilesAuthorizationIntent(db *gorm.DB, authorizationID string, now time.Time) (VFXFilesAuthorizationIntent, error) {
	var intent VFXFilesAuthorizationIntent
	if db == nil || !vfxFilesUUIDRE.MatchString(strings.TrimSpace(authorizationID)) {
		return intent, ErrVFXFilesInvalidBatch
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	err := db.Where("authorization_id = ? AND expires_at > ? AND consumed_at IS NULL", strings.ToLower(strings.TrimSpace(authorizationID)), now.UTC()).Take(&intent).Error
	return intent, err
}

// ConsumeVFXFilesAuthorizationIntent atomically makes a successful OAuth
// return single-use while retaining the identity binding against later reuse.
func ConsumeVFXFilesAuthorizationIntent(db *gorm.DB, authorizationID, productionID, actorID string, now time.Time) error {
	if db == nil || !vfxFilesUUIDRE.MatchString(strings.TrimSpace(authorizationID)) || !vfxFilesUUIDRE.MatchString(strings.TrimSpace(productionID)) || !vfxFilesUUIDRE.MatchString(strings.TrimSpace(actorID)) {
		return ErrVFXFilesInvalidBatch
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result := db.Model(&VFXFilesAuthorizationIntent{}).Where("authorization_id = ? AND production_id = ? AND actor_id = ? AND expires_at > ? AND consumed_at IS NULL",
		strings.ToLower(strings.TrimSpace(authorizationID)),
		strings.ToLower(strings.TrimSpace(productionID)),
		strings.ToLower(strings.TrimSpace(actorID)), now.UTC()).
		Update("consumed_at", now.UTC())
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// VFXFilesSyncOperation stores only the latest bounded preview and sync job
// reference for one Production. It contains no Drive credentials or raw
// Kitsu token; the Plugin remains authoritative for jobs and provider state.
type VFXFilesSyncOperation struct {
	ID                   uint   `gorm:"primaryKey"`
	ProductionID         string `gorm:"not null;uniqueIndex;size:64"`
	ConfigRevision       uint64 `gorm:"not null;default:0"`
	SnapshotID           string `gorm:"size:64"`
	PreviewID            string `gorm:"size:64"`
	PreviewJSON          string `gorm:"type:text"`
	PreviewExpiresAt     *time.Time
	JobID                string `gorm:"size:64"`
	JobJSON              string `gorm:"type:text"`
	ApplyIdempotency     string `gorm:"size:128"`
	RetryIdempotency     string `gorm:"size:128"`
	ReconcileIdempotency string `gorm:"size:128"`
	UpdatedAt            time.Time
}

func (VFXFilesSyncOperation) TableName() string { return "vfx_files_sync_operations" }

// VFXFilesEventInbox durably records raw contract events before HTTP 202.
// PayloadJSON contains only the bounded Plugin business payload (never tokens);
// diagnostics are stable codes and never include raw event text.
type VFXFilesEventInbox struct {
	ID                  uint      `gorm:"primaryKey"`
	ProducerID          string    `gorm:"not null;uniqueIndex:idx_vfx_event_identity,priority:1;size:80"`
	EventID             string    `gorm:"not null;uniqueIndex:idx_vfx_event_identity,priority:2;size:64"`
	PayloadSHA256       string    `gorm:"not null;size:64"`
	PayloadJSON         string    `gorm:"type:text;not null"`
	EventType           string    `gorm:"not null;size:80;index"`
	ProductionID        string    `gorm:"not null;size:64;index"`
	ActivationEpoch     uint64    `gorm:"not null;default:0"`
	DeliveryID          string    `gorm:"size:64;index"`
	DeliveryRevision    uint64    `gorm:"not null;default:0"`
	RecipientTaskID     string    `gorm:"size:64"`
	RecipientTaskTypeID string    `gorm:"size:64"`
	Outcome             string    `gorm:"not null;size:24;index"`
	DiagnosticCode      string    `gorm:"size:80"`
	CreatedAt           time.Time `gorm:"index"`
	ProcessedAt         *time.Time
}

func (VFXFilesEventInbox) TableName() string { return "vfx_files_event_inbox" }

type VFXFilesEventNonce struct {
	ID        uint      `gorm:"primaryKey"`
	KeyID     string    `gorm:"not null;uniqueIndex:idx_vfx_nonce,priority:1;size:80"`
	Nonce     string    `gorm:"not null;uniqueIndex:idx_vfx_nonce,priority:2;size:128"`
	ExpiresAt time.Time `gorm:"not null;index"`
	CreatedAt time.Time
}

func (VFXFilesEventNonce) TableName() string { return "vfx_files_event_nonces" }

type VFXFilesEventConflict struct {
	ID             uint      `gorm:"primaryKey"`
	ProducerID     string    `gorm:"not null;index;size:80"`
	EventID        string    `gorm:"not null;index;size:64"`
	ExistingSHA256 string    `gorm:"not null;size:64"`
	IncomingSHA256 string    `gorm:"not null;size:64"`
	Code           string    `gorm:"not null;size:80"`
	CreatedAt      time.Time `gorm:"index"`
}

func (VFXFilesEventConflict) TableName() string { return "vfx_files_event_conflicts" }

type VFXFilesEventInput struct {
	ProducerID              string
	EventID                 string
	EventType               string
	ProductionID            string
	ActivationEpoch         uint64
	ConfigRevision          uint64
	PreviousActivationEpoch uint64
	NewActivationEpoch      uint64
	DeliveryID              string
	DeliveryRevision        uint64
	RecipientTaskID         string
	RecipientTaskTypeID     string
	Payload                 []byte
}

type VFXFilesEventResult struct {
	EventID string `json:"event_id"`
	Outcome string `json:"outcome"`
	Code    string `json:"code"`
}

// ApplyVFXFilesAuthorityTransition durably changes the mirror while the caller
// holds the exclusive per-Production dispatch barrier. Its successful return
// proves old-epoch dispatches have drained and queued old events are fenced.
func ApplyVFXFilesAuthorityTransition(db *gorm.DB, keyID, nonce string, input VFXFilesEventInput, now time.Time) (VFXFilesEventResult, error) {
	if db == nil || keyID == "" || len(nonce) < 16 || input.ProducerID != "vfx-files-plugin" ||
		(input.EventType != "storage_authority.activation_requested" && input.EventType != "storage_authority.deactivation_requested") ||
		!vfxFilesUUIDRE.MatchString(input.ProductionID) || !vfxFilesUUIDRE.MatchString(input.EventID) ||
		input.ConfigRevision == 0 || input.PreviousActivationEpoch == 0 || input.PreviousActivationEpoch == ^uint64(0) || input.NewActivationEpoch != input.PreviousActivationEpoch+1 {
		return VFXFilesEventResult{}, ErrVFXFilesInvalidBatch
	}
	activating := input.EventType == "storage_authority.activation_requested"
	if activating && input.ActivationEpoch != input.NewActivationEpoch || !activating && input.ActivationEpoch != input.PreviousActivationEpoch {
		return VFXFilesEventResult{}, ErrVFXFilesInvalidBatch
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	payloadHash := sha256.Sum256(input.Payload)
	payloadDigest := hex.EncodeToString(payloadHash[:])
	result := VFXFilesEventResult{EventID: input.EventID, Outcome: VFXFilesEventAccepted, Code: "activation_barrier_complete"}
	err := db.Transaction(func(tx *gorm.DB) error {
		var nonceCount int64
		if err := tx.Model(&VFXFilesEventNonce{}).Where("key_id = ? AND nonce = ?", keyID, nonce).Count(&nonceCount).Error; err != nil {
			return err
		}
		if nonceCount != 0 {
			return ErrVFXFilesEventConflict
		}
		var prior VFXFilesEventInbox
		priorErr := tx.Where("producer_id = ? AND event_id = ?", input.ProducerID, input.EventID).Take(&prior).Error
		if priorErr == nil {
			if prior.PayloadSHA256 != payloadDigest {
				return ErrVFXFilesEventConflict
			}
			result.Outcome, result.Code = VFXFilesEventDuplicate, "duplicate_event"
		} else if !errors.Is(priorErr, gorm.ErrRecordNotFound) {
			return priorErr
		}
		if err := tx.Create(&VFXFilesEventNonce{KeyID: keyID, Nonce: nonce, ExpiresAt: now.Add(10 * time.Minute), CreatedAt: now}).Error; err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "unique") {
				return ErrVFXFilesEventConflict
			}
			return err
		}
		var config VFXFilesProductionConfig
		configErr := tx.Where("production_id = ?", input.ProductionID).First(&config).Error
		if errors.Is(configErr, gorm.ErrRecordNotFound) && activating {
			config = VFXFilesProductionConfig{ProductionID: input.ProductionID, ConfigRevision: input.ConfigRevision, ActivationEpoch: input.PreviousActivationEpoch, Activated: false}
			if err := tx.Create(&config).Error; err != nil {
				return err
			}
		} else if configErr != nil {
			if errors.Is(configErr, gorm.ErrRecordNotFound) {
				return ErrVFXFilesAuthorityTransition
			}
			return configErr
		}
		if activating {
			if config.Activated && config.ActivationEpoch == input.NewActivationEpoch {
				// Idempotent retry after the mirror was durably activated.
			} else if !config.Activated && config.ActivationEpoch == input.PreviousActivationEpoch {
				config.Activated = true
				config.ActivationEpoch = input.NewActivationEpoch
			} else {
				return ErrVFXFilesAuthorityTransition
			}
		} else {
			if !config.Activated && config.ActivationEpoch == input.NewActivationEpoch {
				// Idempotent retry after the mirror was durably fenced.
			} else if config.Activated && config.ActivationEpoch == input.PreviousActivationEpoch {
				config.Activated = false
				config.ActivationEpoch = input.NewActivationEpoch
			} else {
				return ErrVFXFilesAuthorityTransition
			}
		}
		if config.ConfigRevision > input.ConfigRevision {
			return ErrVFXFilesAuthorityTransition
		}
		oldDeliveryEvents := tx.Model(&VFXFilesEventInbox{}).Select("event_id").Where(
			"production_id = ? AND event_type LIKE ? AND activation_epoch <> ?",
			input.ProductionID, "file_delivery.%", input.NewActivationEpoch,
		)
		var unresolvedDispatches int64
		if err := tx.Model(&NotificationDeliveryEvent{}).Where(
			"delivery_source = ? AND production_id = ? AND source_event_id IN (?) AND disposition IN ?",
			"vfx_files", input.ProductionID, oldDeliveryEvents, []string{DeliveryInFlight, DeliveryUnknown},
		).Count(&unresolvedDispatches).Error; err != nil {
			return err
		}
		if unresolvedDispatches != 0 {
			return ErrVFXFilesAuthorityUnresolved
		}
		if err := tx.Model(&NotificationDeliveryEvent{}).Where(
			"delivery_source = ? AND production_id = ? AND source_event_id IN (?) AND disposition IN ?",
			"vfx_files", input.ProductionID, oldDeliveryEvents, []string{DeliveryPending, DeliveryRetryable},
		).Updates(map[string]any{
			"disposition": DeliveryTerminalFailure, "failure_class": "activation_epoch_fenced", "retry_after_at": nil,
		}).Error; err != nil {
			return err
		}
		config.ConfigRevision = input.ConfigRevision
		config.DesiredConfigJSON = fmt.Sprintf(`{"contract_version":"1.0","activation":{"enabled":%t,"notification_epoch":%d}}`, config.Activated, config.ActivationEpoch)
		config.PendingActivationEpoch = 0
		if err := tx.Save(&config).Error; err != nil {
			return err
		}
		if err := tx.Model(&VFXFilesEventInbox{}).Where(
			"production_id = ? AND event_type LIKE ? AND outcome = ? AND processed_at IS NULL AND activation_epoch <> ?",
			input.ProductionID, "file_delivery.%", VFXFilesEventAccepted, input.NewActivationEpoch,
		).Updates(map[string]any{
			"outcome": VFXFilesEventQuarantined, "diagnostic_code": "activation_epoch_fenced", "processed_at": now,
		}).Error; err != nil {
			return err
		}
		if priorErr != nil {
			row := VFXFilesEventInbox{
				ProducerID: input.ProducerID, EventID: input.EventID, PayloadSHA256: payloadDigest,
				PayloadJSON: string(input.Payload), EventType: input.EventType, ProductionID: input.ProductionID,
				ActivationEpoch: input.ActivationEpoch, Outcome: VFXFilesEventAccepted,
				DiagnosticCode: "activation_barrier_complete", CreatedAt: now, ProcessedAt: &now,
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return result, err
}

// VFXFilesEventDiagnostics contains counts only; payloads and identity values
// are deliberately excluded from the operator-facing view.
type VFXFilesEventDiagnostics struct {
	PendingDelivery  int64
	Quarantined      int64
	IdentityConflict int64
}

func ReadVFXFilesEventDiagnostics(db *gorm.DB, productionID string) (VFXFilesEventDiagnostics, error) {
	var result VFXFilesEventDiagnostics
	if db == nil || !vfxFilesUUIDRE.MatchString(strings.TrimSpace(productionID)) {
		return result, gorm.ErrInvalidData
	}
	if err := db.Model(&VFXFilesEventInbox{}).Where("production_id = ? AND event_type LIKE ? AND outcome = ? AND processed_at IS NULL", productionID, "file_delivery.%", VFXFilesEventAccepted).Count(&result.PendingDelivery).Error; err != nil {
		return VFXFilesEventDiagnostics{}, err
	}
	if err := db.Model(&VFXFilesEventInbox{}).Where("production_id = ? AND outcome = ?", productionID, VFXFilesEventQuarantined).Count(&result.Quarantined).Error; err != nil {
		return VFXFilesEventDiagnostics{}, err
	}
	productionEvents := db.Model(&VFXFilesEventInbox{}).Select("event_id").Where("production_id = ?", productionID)
	if err := db.Model(&VFXFilesEventConflict{}).Where("producer_id = ? AND event_id IN (?)", "vfx-files-plugin", productionEvents).Count(&result.IdentityConflict).Error; err != nil {
		return VFXFilesEventDiagnostics{}, err
	}
	return result, nil
}

func PendingVFXFilesDeliveryEvents(db *gorm.DB, limit int) ([]VFXFilesEventInbox, error) {
	if db == nil {
		return nil, gorm.ErrInvalidDB
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	var rows []VFXFilesEventInbox
	err := pendingVFXFilesDeliveryEventsQuery(db, time.Now().UTC()).Order("vfx_files_event_inbox.id asc").Limit(limit).Find(&rows).Error
	return rows, err
}

// VFXFilesDeliveryEventHighWatermark snapshots the backlog boundary for one
// processing pass. New arrivals are left for the next pass.
func VFXFilesDeliveryEventHighWatermark(db *gorm.DB, now time.Time) (uint, error) {
	if db == nil {
		return 0, gorm.ErrInvalidDB
	}
	var highWatermark uint
	err := pendingVFXFilesDeliveryEventsQuery(db, now).
		Select("COALESCE(MAX(id), 0)").Scan(&highWatermark).Error
	return highWatermark, err
}

// PendingVFXFilesDeliveryEventsPage keyset-pages every due accepted event up
// to a caller's fixed high watermark. The only temporary exclusion is an
// explicit future Retry-After persisted in its delivery journal; there is no
// age-based expiry, so old rows become eligible again when that deadline ends.
func PendingVFXFilesDeliveryEventsPage(db *gorm.DB, afterID, highWatermark uint, limit int, now time.Time) ([]VFXFilesEventInbox, error) {
	if db == nil {
		return nil, gorm.ErrInvalidDB
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	if highWatermark == 0 || afterID >= highWatermark {
		return []VFXFilesEventInbox{}, nil
	}
	var rows []VFXFilesEventInbox
	err := pendingVFXFilesDeliveryEventsQuery(db, now).
		Where("vfx_files_event_inbox.id > ? AND vfx_files_event_inbox.id <= ?", afterID, highWatermark).
		Order("id asc").Limit(limit).Find(&rows).Error
	return rows, err
}

func pendingVFXFilesDeliveryEventsQuery(db *gorm.DB, now time.Time) *gorm.DB {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return db.Model(&VFXFilesEventInbox{}).
		Where("vfx_files_event_inbox.outcome = ? AND vfx_files_event_inbox.processed_at IS NULL AND vfx_files_event_inbox.event_type LIKE ?", VFXFilesEventAccepted, "file_delivery.%").
		Where("NOT EXISTS (SELECT 1 FROM notification_delivery_events WHERE delivery_source = ? AND source_event_id = vfx_files_event_inbox.event_id AND disposition = ? AND retry_after_at > ?)", "vfx_files", DeliveryRetryable, now.UTC())
}

// BeginVFXFilesActivationMirror records the exact next epoch before the
// external activation request. Only events for this explicitly initiated,
// single-step transition may receive a transient retry response.
func BeginVFXFilesActivationMirror(db *gorm.DB, productionID string) error {
	productionID = strings.ToLower(strings.TrimSpace(productionID))
	if db == nil || !vfxFilesUUIDRE.MatchString(strings.TrimSpace(productionID)) {
		return ErrVFXFilesInvalidBatch
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var config VFXFilesProductionConfig
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("production_id = ?", strings.TrimSpace(productionID)).First(&config).Error; err != nil {
			return err
		}
		if config.PendingActivationEpoch != 0 {
			return ErrVFXFilesActivationPending
		}
		if config.ActivationEpoch >= 9_000_000_000_000_000 {
			return ErrVFXFilesInvalidBatch
		}
		result := tx.Model(&VFXFilesProductionConfig{}).Where("id = ? AND pending_activation_epoch = 0", config.ID).Update("pending_activation_epoch", config.ActivationEpoch+1)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrVFXFilesActivationPending
		}
		return nil
	})
}

func CancelVFXFilesActivationMirror(db *gorm.DB, productionID string) error {
	productionID = strings.ToLower(strings.TrimSpace(productionID))
	if db == nil || !vfxFilesUUIDRE.MatchString(strings.TrimSpace(productionID)) {
		return ErrVFXFilesInvalidBatch
	}
	return db.Model(&VFXFilesProductionConfig{}).Where("production_id = ?", strings.TrimSpace(productionID)).Update("pending_activation_epoch", 0).Error
}

func MarkVFXFilesEventProcessed(db *gorm.DB, eventID string, now time.Time) (bool, error) {
	if db == nil || strings.TrimSpace(eventID) == "" {
		return false, gorm.ErrInvalidData
	}
	result := db.Model(&VFXFilesEventInbox{}).Where("event_id = ? AND outcome = ? AND processed_at IS NULL", strings.TrimSpace(eventID), VFXFilesEventAccepted).Update("processed_at", now.UTC())
	return result.RowsAffected == 1, result.Error
}

func QuarantineVFXFilesEvent(db *gorm.DB, eventID, diagnostic string, now time.Time) error {
	if db == nil || strings.TrimSpace(eventID) == "" || len(diagnostic) > 80 || !regexp.MustCompile(`^[a-z][a-z0-9_]{0,79}$`).MatchString(diagnostic) {
		return gorm.ErrInvalidData
	}
	return db.Model(&VFXFilesEventInbox{}).Where("event_id = ? AND outcome = ? AND processed_at IS NULL", strings.TrimSpace(eventID), VFXFilesEventAccepted).Updates(map[string]any{
		"outcome": VFXFilesEventQuarantined, "diagnostic_code": diagnostic, "processed_at": now.UTC(),
	}).Error
}

// AcceptVFXFilesEvents records a signed batch and consumes its nonce atomically.
// A duplicate event with identical body is harmless; a conflicting body is
// retained as a safe conflict diagnostic and cannot replace the original.
func AcceptVFXFilesEvents(db *gorm.DB, keyID, nonce string, events []VFXFilesEventInput, now time.Time) ([]VFXFilesEventResult, error) {
	if db == nil || strings.TrimSpace(keyID) == "" || strings.TrimSpace(nonce) == "" || len(events) == 0 || len(events) > 100 {
		return nil, ErrVFXFilesInvalidBatch
	}
	results := make([]VFXFilesEventResult, 0, len(events))
	// Preserve the Plugin's batch order. Sorting by revision or Event ID would
	// hide an out-of-order status stream and could make stale state look valid.
	ordered := append([]VFXFilesEventInput(nil), events...)
	seenEventIDs := make(map[string]struct{}, len(ordered))
	for index := range ordered {
		ordered[index].ProductionID = strings.ToLower(strings.TrimSpace(ordered[index].ProductionID))
		event := ordered[index]
		if _, exists := seenEventIDs[event.EventID]; exists {
			return nil, ErrVFXFilesInvalidBatch
		}
		seenEventIDs[event.EventID] = struct{}{}
	}
	byEventID := make(map[string]VFXFilesEventResult, len(events))
	activationPending := make(map[string]struct{})
	err := db.Transaction(func(tx *gorm.DB) error {
		// Hold only the exact epoch recorded by an in-progress activation.
		// Other events in the same signed batch still get durable accepted or
		// quarantined outcomes before the request asks the Plugin to retry.
		for _, input := range ordered {
			var nonceCount int64
			if err := tx.Model(&VFXFilesEventNonce{}).Where("key_id = ? AND nonce = ?", keyID, nonce).Count(&nonceCount).Error; err != nil {
				return err
			}
			if nonceCount != 0 {
				return ErrVFXFilesEventConflict
			}
			var existingCount int64
			if err := tx.Model(&VFXFilesEventInbox{}).Where("producer_id = ? AND event_id = ?", input.ProducerID, input.EventID).Count(&existingCount).Error; err != nil {
				return err
			}
			if existingCount != 0 {
				continue
			}
			var config VFXFilesProductionConfig
			err := tx.Where("production_id = ?", input.ProductionID).First(&config).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if config.PendingActivationEpoch != 0 && config.ActivationEpoch < 9_000_000_000_000_000 &&
				config.PendingActivationEpoch == config.ActivationEpoch+1 && input.ActivationEpoch == config.PendingActivationEpoch {
				activationPending[input.EventID] = struct{}{}
			}
		}
		row := VFXFilesEventNonce{KeyID: keyID, Nonce: nonce, ExpiresAt: now.Add(10 * time.Minute), CreatedAt: now}
		if err := tx.Create(&row).Error; err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "unique") {
				return ErrVFXFilesEventConflict
			}
			return err
		}
		for _, input := range ordered {
			if _, pending := activationPending[input.EventID]; pending {
				continue
			}
			if input.ProducerID == "" || input.EventID == "" || input.ProductionID == "" || input.EventType == "" || len(input.Payload) == 0 {
				return ErrVFXFilesInvalidBatch
			}
			sum := sha256.Sum256(input.Payload)
			payloadHash := hex.EncodeToString(sum[:])
			var existing VFXFilesEventInbox
			err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("producer_id = ? AND event_id = ?", input.ProducerID, input.EventID).First(&existing).Error
			if err == nil {
				if existing.PayloadSHA256 == payloadHash {
					byEventID[input.EventID] = VFXFilesEventResult{EventID: input.EventID, Outcome: VFXFilesEventDuplicate, Code: "duplicate_event"}
					continue
				}
				if err := tx.Create(&VFXFilesEventConflict{ProducerID: input.ProducerID, EventID: input.EventID, ExistingSHA256: existing.PayloadSHA256, IncomingSHA256: payloadHash, Code: "event_identity_conflict", CreatedAt: now}).Error; err != nil {
					return err
				}
				byEventID[input.EventID] = VFXFilesEventResult{EventID: input.EventID, Outcome: VFXFilesEventQuarantined, Code: "event_identity_conflict"}
				continue
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			outcome, code := VFXFilesEventAccepted, "accepted"
			var config VFXFilesProductionConfig
			configErr := tx.Where("production_id = ?", input.ProductionID).First(&config).Error
			if errors.Is(configErr, gorm.ErrRecordNotFound) || configErr == nil && (!config.Activated || config.ActivationEpoch == 0 || config.ActivationEpoch != input.ActivationEpoch) {
				outcome, code = VFXFilesEventQuarantined, "activation_epoch_mismatch"
			} else if configErr != nil {
				return configErr
			}
			if outcome == VFXFilesEventAccepted && input.DeliveryID != "" && input.DeliveryRevision > 0 {
				// A previously quarantined gap/transition conflict is durable
				// evidence that this stream arrived out of order. Do not let a later
				// lower/equal revision advance accepted history past that evidence.
				var priorOrderingConflict int64
				if err := tx.Model(&VFXFilesEventInbox{}).
					Where("production_id = ? AND delivery_id = ? AND activation_epoch = ? AND outcome = ? AND delivery_revision >= ? AND diagnostic_code IN ?",
						input.ProductionID, input.DeliveryID, input.ActivationEpoch, VFXFilesEventQuarantined, input.DeliveryRevision,
						[]string{"delivery_revision_gap", "invalid_delivery_transition", "delivery_order_conflict"}).
					Count(&priorOrderingConflict).Error; err != nil {
					return err
				}
				if priorOrderingConflict > 0 {
					outcome, code = VFXFilesEventQuarantined, "delivery_order_conflict"
				} else {
					var latest VFXFilesEventInbox
					readErr := tx.Where("production_id = ? AND delivery_id = ? AND outcome = ?", input.ProductionID, input.DeliveryID, VFXFilesEventAccepted).
						Order("delivery_revision desc").Order("id desc").First(&latest).Error
					if readErr != nil && !errors.Is(readErr, gorm.ErrRecordNotFound) {
						return readErr
					}
					var previous *VFXFilesEventInbox
					if readErr == nil {
						previous = &latest
					}
					if diagnostic := validateVFXFilesDeliveryTransition(previous, input); diagnostic != "" {
						outcome, code = VFXFilesEventQuarantined, diagnostic
					}
				}
			}
			stored := VFXFilesEventInbox{ProducerID: input.ProducerID, EventID: input.EventID, PayloadSHA256: payloadHash, PayloadJSON: string(input.Payload), EventType: input.EventType, ProductionID: input.ProductionID, ActivationEpoch: input.ActivationEpoch, DeliveryID: input.DeliveryID, DeliveryRevision: input.DeliveryRevision, RecipientTaskID: input.RecipientTaskID, RecipientTaskTypeID: input.RecipientTaskTypeID, Outcome: outcome, DiagnosticCode: code, CreatedAt: now}
			if err := tx.Create(&stored).Error; err != nil {
				return err
			}
			byEventID[input.EventID] = VFXFilesEventResult{EventID: input.EventID, Outcome: outcome, Code: code}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(activationPending) > 0 {
		return nil, ErrVFXFilesActivationPending
	}
	for _, input := range events {
		results = append(results, byEventID[input.EventID])
	}
	return results, nil
}

// validateVFXFilesDeliveryTransition treats delivery_revision as the
// recipient Delivery stream sequence, independent of output_package.version.
// Status transitions may share a Delivery revision; only a replacement
// Package's `revised` event advances the recipient stream sequence.
func validateVFXFilesDeliveryTransition(previous *VFXFilesEventInbox, incoming VFXFilesEventInput) string {
	status, ok := vfxFilesDeliveryStatus(incoming.EventType)
	if !ok {
		return "invalid_delivery_transition"
	}
	if previous == nil {
		if incoming.DeliveryRevision == 1 && (status == "created" || status == "ready") {
			return ""
		}
		return "delivery_revision_gap"
	}
	previousStatus, ok := vfxFilesDeliveryStatus(previous.EventType)
	if !ok {
		return "invalid_delivery_transition"
	}
	if incoming.DeliveryRevision < previous.DeliveryRevision {
		return "stale_delivery_revision"
	}
	if incoming.DeliveryRevision == previous.DeliveryRevision {
		valid := false
		switch previousStatus {
		case "created":
			valid = status == "ready" || status == "accepted" || status == "needs_fix"
		case "ready", "revised":
			valid = status == "accepted" || status == "needs_fix"
		}
		if valid {
			return ""
		}
		return "invalid_delivery_transition"
	}
	if incoming.DeliveryRevision != previous.DeliveryRevision+1 {
		return "delivery_revision_gap"
	}
	if status == "revised" && (previousStatus == "needs_fix" || previousStatus == "accepted") {
		return ""
	}
	return "invalid_delivery_transition"
}

func vfxFilesDeliveryStatus(eventType string) (string, bool) {
	if !strings.HasPrefix(eventType, "file_delivery.") {
		return "", false
	}
	status := strings.TrimPrefix(eventType, "file_delivery.")
	switch status {
	case "created", "ready", "accepted", "needs_fix", "revised":
		return status, true
	default:
		return "", false
	}
}

func PruneVFXFilesEventNonces(db *gorm.DB, now time.Time, limit int) (int64, error) {
	if db == nil {
		return 0, gorm.ErrInvalidDB
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var rows []VFXFilesEventNonce
	if err := db.Where("expires_at < ?", now).Order("id asc").Limit(limit).Find(&rows).Error; err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	ids := make([]uint, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	result := db.Where("id IN ? AND expires_at < ?", ids, now).Delete(&VFXFilesEventNonce{})
	return result.RowsAffected, result.Error
}
