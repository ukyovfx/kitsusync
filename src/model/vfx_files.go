package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func StoreVFXFilesSyncPreview(db *gorm.DB, productionID, snapshotID string, configRevision uint64, preview []byte) error {
	if db == nil || !vfxFilesUUIDRE.MatchString(strings.TrimSpace(productionID)) || !vfxFilesUUIDRE.MatchString(strings.TrimSpace(snapshotID)) || configRevision == 0 || len(preview) == 0 || len(preview) > 512<<10 {
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
	row := VFXFilesSyncOperation{ProductionID: strings.ToLower(productionID), ConfigRevision: configRevision, SnapshotID: strings.ToLower(snapshotID), PreviewID: strings.ToLower(value.PreviewID), PreviewJSON: string(preview), PreviewExpiresAt: &expires, JobID: "", JobJSON: "", ApplyIdempotency: "", RetryIdempotency: "", ReconcileIdempotency: "", UpdatedAt: time.Now().UTC()}
	return db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "production_id"}}, DoUpdates: clause.AssignmentColumns([]string{"config_revision", "snapshot_id", "preview_id", "preview_json", "preview_expires_at", "job_id", "job_json", "apply_idempotency", "retry_idempotency", "reconcile_idempotency", "updated_at"})}).Create(&row).Error
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
			result := tx.Model(&row).Update(column, idempotency)
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
	}
	decoder := json.NewDecoder(strings.NewReader(string(job)))
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF || value.ContractVersion != "1.0" || !vfxFilesUUIDRE.MatchString(value.ID) || !strings.EqualFold(value.ProductionID, productionID) || value.ConfigRevision == 0 || value.Progress.Completed < 0 || value.Progress.Total < value.Progress.Completed {
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
		State     string `json:"state"`
		UpdatedAt string `json:"updated_at"`
	}
	_ = json.Unmarshal([]byte(previous.JobJSON), &previousPayload)
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
	result := db.Model(&VFXFilesSyncOperation{}).Where("production_id = ? AND config_revision = ?", strings.ToLower(productionID), value.ConfigRevision).Updates(updates)
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
	ErrVFXFilesEventConflict  = errors.New("VFX Files event identity conflict")
	ErrVFXFilesInvalidBatch   = errors.New("VFX Files event batch is invalid")
	ErrVFXFilesConfigConflict = errors.New("VFX Files desired config revision conflict")
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
	productionID = strings.TrimSpace(productionID)
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
	err := db.Transaction(func(tx *gorm.DB) error {
		var existing VFXFilesProductionConfig
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("production_id = ?", productionID).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			row := VFXFilesProductionConfig{ProductionID: productionID, DesiredConfigJSON: string(body), ConfigRevision: config.ConfigRevision, ActivationEpoch: config.Activation.NotificationEpoch, Activated: config.Activation.Enabled, UpdatedAt: time.Now().UTC()}
			return tx.Create(&row).Error
		}
		if err != nil {
			return err
		}
		if config.ConfigRevision < existing.ConfigRevision || config.ConfigRevision == existing.ConfigRevision && (existing.DesiredConfigJSON != string(body) || existing.ActivationEpoch != config.Activation.NotificationEpoch || existing.Activated != config.Activation.Enabled) {
			return ErrVFXFilesConfigConflict
		}
		result := tx.Model(&VFXFilesProductionConfig{}).Where("id = ? AND config_revision <= ?", existing.ID, config.ConfigRevision).Updates(map[string]any{
			"desired_config_json": string(body), "config_revision": config.ConfigRevision, "activation_epoch": config.Activation.NotificationEpoch,
			"activated": config.Activation.Enabled, "updated_at": time.Now().UTC(),
		})
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

// DisableVFXFilesEventIntake pauses local event acceptance and delivery before
// an external deactivation request. If that request has an unknown outcome,
// KitsuSync remains fail-closed until the desired-config mirror is reconciled.
func DisableVFXFilesEventIntake(db *gorm.DB, productionID string) error {
	productionID = strings.TrimSpace(productionID)
	if db == nil || !vfxFilesUUIDRE.MatchString(productionID) {
		return ErrVFXFilesInvalidBatch
	}
	result := db.Model(&VFXFilesProductionConfig{}).Where("production_id = ?", productionID).Update("activated", false)
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

// VFXFilesProductionConfig stores KitsuSync-owned desired integration state;
// it never contains OAuth credentials or the Plugin's credential reference.
type VFXFilesProductionConfig struct {
	ID                uint   `gorm:"primaryKey"`
	ProductionID      string `gorm:"not null;uniqueIndex;size:64"`
	DesiredConfigJSON string `gorm:"type:text;not null;default:''"`
	ConfigRevision    uint64 `gorm:"not null;default:0"`
	ActivationEpoch   uint64 `gorm:"not null;default:0"`
	Activated         bool   `gorm:"not null;default:false;index"`
	UpdatedAt         time.Time
}

func (VFXFilesProductionConfig) TableName() string { return "vfx_files_production_configs" }

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
	ProducerID          string
	EventID             string
	EventType           string
	ProductionID        string
	ActivationEpoch     uint64
	DeliveryID          string
	DeliveryRevision    uint64
	RecipientTaskID     string
	RecipientTaskTypeID string
	Payload             []byte
}

type VFXFilesEventResult struct {
	EventID string `json:"event_id"`
	Outcome string `json:"outcome"`
	Code    string `json:"code"`
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
	err := db.Where("outcome = ? AND processed_at IS NULL AND event_type LIKE ?", VFXFilesEventAccepted, "file_delivery.%").Order("id asc").Limit(limit).Find(&rows).Error
	return rows, err
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
	ordered := append([]VFXFilesEventInput(nil), events...)
	seenEventIDs := make(map[string]struct{}, len(ordered))
	for _, event := range ordered {
		if _, exists := seenEventIDs[event.EventID]; exists {
			return nil, ErrVFXFilesInvalidBatch
		}
		seenEventIDs[event.EventID] = struct{}{}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].DeliveryID != ordered[j].DeliveryID {
			return ordered[i].DeliveryID < ordered[j].DeliveryID
		}
		if ordered[i].DeliveryRevision != ordered[j].DeliveryRevision {
			return ordered[i].DeliveryRevision < ordered[j].DeliveryRevision
		}
		return ordered[i].EventID < ordered[j].EventID
	})
	byEventID := make(map[string]VFXFilesEventResult, len(events))
	err := db.Transaction(func(tx *gorm.DB) error {
		row := VFXFilesEventNonce{KeyID: keyID, Nonce: nonce, ExpiresAt: now.Add(10 * time.Minute), CreatedAt: now}
		if err := tx.Create(&row).Error; err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "unique") {
				return ErrVFXFilesEventConflict
			}
			return err
		}
		for _, input := range ordered {
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
			if input.DeliveryID != "" && input.DeliveryRevision > 0 {
				var latest VFXFilesEventInbox
				readErr := tx.Where("production_id = ? AND delivery_id = ?", input.ProductionID, input.DeliveryID).Order("delivery_revision desc").First(&latest).Error
				if readErr != nil && !errors.Is(readErr, gorm.ErrRecordNotFound) {
					return readErr
				}
				if readErr == nil && input.DeliveryRevision <= latest.DeliveryRevision {
					outcome, code = VFXFilesEventQuarantined, "stale_delivery_revision"
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
	for _, input := range events {
		results = append(results, byEventID[input.EventID])
	}
	return results, nil
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
