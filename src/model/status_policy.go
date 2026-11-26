package model

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gookit/slog"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Determination model (redesign core v2, 2026-11-23). Kitsu statuses are
// classified once into natures; whether a status speaks is derived from its
// nature, recipients are derived from the nature, and every decision is
// logged. The only human judgments left are the exception queue entries the
// derivation cannot settle.

// Status natures. The forum roles of the previous model map one to one:
// review=wfa, retake=retake, done=done, wip=wip (tag only).
const (
	StatusNatureReview    = "review"
	StatusNatureRetake    = "retake"
	StatusNatureDone      = "done"
	StatusNatureWIP       = "wip"
	StatusNatureNone      = "none"
	StatusNatureAmbiguous = "ambiguous"
)

// StatusPolicyEnabled reports whether a status of this nature produces a
// signal. The recommended defaults are fixed by the operating model: review,
// retake and done speak; wip only moves the tag; none and ambiguous stay
// silent (ambiguous goes to the exception queue instead).
func StatusPolicyEnabled(nature string) bool {
	switch nature {
	case StatusNatureReview, StatusNatureRetake, StatusNatureDone:
		return true
	}
	return false
}

// StatusNatureRole maps a nature to the forum role used for tags and card
// colors. Ambiguous and none have no role.
func StatusNatureRole(nature string) string {
	switch nature {
	case StatusNatureReview:
		return ForumRoleWFA
	case StatusNatureRetake:
		return ForumRoleRetake
	case StatusNatureDone:
		return ForumRoleDone
	case StatusNatureWIP:
		return ForumRoleWIP
	}
	return ""
}

// StatusNatureIntent maps a nature to the forum signal intent it produces.
func StatusNatureIntent(nature string) string {
	switch nature {
	case StatusNatureReview:
		return ForumIntentStatusWFA
	case StatusNatureRetake:
		return ForumIntentStatusRetake
	case StatusNatureDone:
		return ForumIntentStatusDone
	}
	return ""
}

// ClassifyStatusName derives the nature of a Kitsu status from its name. The
// dictionary is deliberately small and explicit; anything it cannot settle is
// ambiguous, never silently none, so a human decides it once in the
// exception queue.
func ClassifyStatusName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "waiting for approval":
		return StatusNatureReview
	case "retake":
		return StatusNatureRetake
	case "done", "approved":
		return StatusNatureDone
	case "work in progress":
		return StatusNatureWIP
	case "neutral", "todo", "ready to start":
		return StatusNatureNone
	}
	return StatusNatureAmbiguous
}

// StudioStatusPolicy is the studio-common classification of one Kitsu
// status. It is keyed by the Kitsu status ID so a rename keeps the row; the
// name is a snapshot for display and rename detection.
type StudioStatusPolicy struct {
	StatusID   string `gorm:"primaryKey;size:64"`
	StatusName string `gorm:"size:191"`
	Nature     string `gorm:"size:16;not null"`
	Manual     bool   `gorm:"not null;default:false"`
	UpdatedAt  time.Time
}

func (StudioStatusPolicy) TableName() string { return "studio_status_policies" }

// LoadStudioStatusPolicies returns every policy keyed by status ID.
func LoadStudioStatusPolicies(db *gorm.DB) (map[string]StudioStatusPolicy, error) {
	var rows []StudioStatusPolicy
	if err := db.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]StudioStatusPolicy, len(rows))
	for _, row := range rows {
		out[row.StatusID] = row
	}
	return out, nil
}

// SetStudioStatusPolicyNature records a human classification from the
// exception queue (or the studio settings page) and marks it manual so the
// reconcile never overwrites it.
func SetStudioStatusPolicyNature(db *gorm.DB, statusID, statusName, nature string) error {
	row := StudioStatusPolicy{StatusID: statusID, StatusName: statusName, Nature: nature, Manual: true, UpdatedAt: time.Now().UTC()}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "status_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"status_name", "nature", "manual", "updated_at"}),
	}).Create(&row).Error
}

// StatusPolicyRoles derives the status ID -> forum role map the digest and
// the planner need, from the policies: a status has a role when its nature
// maps to one and the nature is enabled (wip keeps its tag role: it is a
// display role, not a signal).
func StatusPolicyRoles(policies map[string]StudioStatusPolicy) map[string]string {
	out := map[string]string{}
	for id, policy := range policies {
		if role := StatusNatureRole(policy.Nature); role != "" {
			out[id] = role
		}
	}
	return out
}

// --- Task routes (handoff wiring) ---

// StudioTaskRoute is the studio-common handoff wiring: when a task of
// FromTaskTypeID finishes (done nature), the assignees of the same entity's
// NotifyTaskTypeIDs tasks are told. NotifyTaskTypeIDs is a comma-separated
// list of Kitsu task type IDs; the value "none" means this task type hands
// off to nobody.
type StudioTaskRoute struct {
	FromTaskTypeID    string `gorm:"primaryKey;size:64"`
	FromTaskTypeName  string `gorm:"size:191"`
	NotifyTaskTypeIDs string `gorm:"type:text"`
	UpdatedAt         time.Time
}

func (StudioTaskRoute) TableName() string { return "studio_task_routes" }

// ProductionTaskRoute overrides the studio route for one Production. An
// override with NotifyTaskTypeIDs "none" suppresses the handoff for that
// Production only.
type ProductionTaskRoute struct {
	ProductionID      string `gorm:"primaryKey;size:64;index:idx_production_task_route,priority:1"`
	FromTaskTypeID    string `gorm:"primaryKey;size:64;index:idx_production_task_route,priority:2"`
	FromTaskTypeName  string `gorm:"size:191"`
	NotifyTaskTypeIDs string `gorm:"type:text"`
	UpdatedAt         time.Time
}

func (ProductionTaskRoute) TableName() string { return "production_task_routes" }

// TaskRouteNone is the wiring value that means "hands off to nobody".
const TaskRouteNone = "none"

// splitTaskRouteIDs decodes a stored wiring value: the "none" sentinel and
// an empty value both mean an empty notify list.
func splitTaskRouteIDs(raw string) []string {
	if strings.TrimSpace(raw) == TaskRouteNone {
		return nil
	}
	return SplitHandoffNotifyIDs(raw)
}

// EffectiveTaskRoute resolves the wiring for one (production, task type):
// a production override wins over the studio default. The origin is reported
// for the decision log.
func EffectiveTaskRoute(db *gorm.DB, productionID, fromTaskTypeID string) (notifyIDs []string, origin string, found bool, err error) {
	var production ProductionTaskRoute
	err = db.Where("production_id = ? AND from_task_type_id = ?", productionID, fromTaskTypeID).First(&production).Error
	if err == nil {
		return splitTaskRouteIDs(production.NotifyTaskTypeIDs), "override", true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, "", false, err
	}
	var studio StudioTaskRoute
	err = db.Where("from_task_type_id = ?", fromTaskTypeID).First(&studio).Error
	if err == nil {
		return splitTaskRouteIDs(studio.NotifyTaskTypeIDs), "studio", true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, "", false, nil
	}
	return nil, "", false, err
}

// SaveStudioTaskRoute upserts one studio route.
func SaveStudioTaskRoute(db *gorm.DB, route StudioTaskRoute) error {
	route.UpdatedAt = time.Now().UTC()
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "from_task_type_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"from_task_type_name", "notify_task_type_ids", "updated_at"}),
	}).Create(&route).Error
}

// DeleteStudioTaskRoute removes a studio route.
func DeleteStudioTaskRoute(db *gorm.DB, fromTaskTypeID string) error {
	return db.Where("from_task_type_id = ?", fromTaskTypeID).Delete(&StudioTaskRoute{}).Error
}

// SaveProductionTaskRoute upserts one production override.
func SaveProductionTaskRoute(db *gorm.DB, route ProductionTaskRoute) error {
	route.UpdatedAt = time.Now().UTC()
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "production_id"}, {Name: "from_task_type_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"from_task_type_name", "notify_task_type_ids", "updated_at"}),
	}).Create(&route).Error
}

// DeleteProductionTaskRoute removes an override so the studio default applies
// again.
func DeleteProductionTaskRoute(db *gorm.DB, productionID, fromTaskTypeID string) error {
	return db.Where("production_id = ? AND from_task_type_id = ?", productionID, fromTaskTypeID).Delete(&ProductionTaskRoute{}).Error
}

// LoadStudioTaskRoutes returns every studio route.
func LoadStudioTaskRoutes(db *gorm.DB) ([]StudioTaskRoute, error) {
	var rows []StudioTaskRoute
	err := db.Order("from_task_type_name asc").Find(&rows).Error
	return rows, err
}

// LoadProductionTaskRoutes returns every override of one Production.
func LoadProductionTaskRoutes(db *gorm.DB, productionID string) ([]ProductionTaskRoute, error) {
	var rows []ProductionTaskRoute
	err := db.Where("production_id = ?", productionID).Order("from_task_type_name asc").Find(&rows).Error
	return rows, err
}

// --- Exception queue ---

// Exception kinds. Each is a judgment only a human can make; everything else
// is derived.
const (
	ExceptionStatusAmbiguous     = "status_ambiguous"
	ExceptionStatusRenamed       = "status_renamed"
	ExceptionRecipientUnresolved = "recipient_unresolved"
	ExceptionNoRoute             = "no_route"
	ExceptionReviewStale         = "review_stale"

	ExceptionStateOpen      = "open"
	ExceptionStateResolved  = "resolved"
	ExceptionStateDismissed = "dismissed"
)

// AdminException is one open judgment in the admin exception queue. The same
// (kind, subject) aggregates into one row with a seen count instead of one
// row per occurrence.
type AdminException struct {
	ID           uint   `gorm:"primaryKey"`
	Kind         string `gorm:"size:32;not null;uniqueIndex:idx_admin_exception_subject,priority:1"`
	SubjectID    string `gorm:"size:64;not null;uniqueIndex:idx_admin_exception_subject,priority:2"`
	SubjectName  string `gorm:"size:191"`
	ProductionID string `gorm:"size:64;uniqueIndex:idx_admin_exception_subject,priority:3"`
	Detail       string `gorm:"type:text"`
	State        string `gorm:"size:16;not null"`
	FirstSeenAt  time.Time
	LastSeenAt   time.Time
	SeenCount    int `gorm:"not null;default:1"`
}

func (AdminException) TableName() string { return "admin_exceptions" }

// RaiseAdminException records an occurrence, aggregating repeats of the same
// (kind, subject, production) into the existing open row. A previously
// resolved or dismissed row is reopened only by a genuinely new occurrence
// after its resolution; callers that re-derive the same state every poll
// (like the stale-review check) resolve their own rows when the state clears.
func RaiseAdminException(db *gorm.DB, kind, subjectID, subjectName, productionID, detail string) error {
	now := time.Now().UTC()
	var existing AdminException
	err := db.Where("kind = ? AND subject_id = ? AND production_id = ?", kind, subjectID, productionID).First(&existing).Error
	if err == nil {
		if existing.State != ExceptionStateOpen {
			return nil
		}
		return db.Model(&AdminException{}).Where("id = ?", existing.ID).Updates(map[string]interface{}{
			"subject_name": subjectName,
			"detail":       detail,
			"last_seen_at": now,
			"seen_count":   gorm.Expr("seen_count + 1"),
		}).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return db.Create(&AdminException{
		Kind: kind, SubjectID: subjectID, SubjectName: subjectName, ProductionID: productionID,
		Detail: detail, State: ExceptionStateOpen, FirstSeenAt: now, LastSeenAt: now, SeenCount: 1,
	}).Error
}

// ResolveAdminException marks one row resolved (the human acted, or the
// state that raised it cleared by itself).
func ResolveAdminException(db *gorm.DB, id uint, dismissed bool) error {
	state := ExceptionStateResolved
	if dismissed {
		state = ExceptionStateDismissed
	}
	return db.Model(&AdminException{}).Where("id = ? AND state = ?", id, ExceptionStateOpen).Update("state", state).Error
}

// AutoResolveAdminExceptions closes open rows of a kind for a subject whose
// condition cleared (e.g. the task left WIP, the status got classified).
func AutoResolveAdminExceptions(db *gorm.DB, kind, subjectID string) error {
	return db.Model(&AdminException{}).
		Where("kind = ? AND subject_id = ? AND state = ?", kind, subjectID, ExceptionStateOpen).
		Update("state", ExceptionStateResolved).Error
}

// OpenAdminExceptions lists the open queue, oldest first.
func OpenAdminExceptions(db *gorm.DB) ([]AdminException, error) {
	var rows []AdminException
	err := db.Where("state = ?", ExceptionStateOpen).Order("first_seen_at asc, id asc").Find(&rows).Error
	return rows, err
}

// --- Decision log ---

// NotificationDecision is one row of the "why did this person get (or not
// get) this notification" log. RuleID names the rule that fired; Facts is a
// JSON snapshot of the inputs the rule saw.
type NotificationDecision struct {
	ID             uint      `gorm:"primaryKey"`
	CreatedAt      time.Time `gorm:"index:idx_notification_decision_created"`
	ProductionID   string    `gorm:"size:64"`
	TaskID         string    `gorm:"size:64"`
	TaskTypeName   string    `gorm:"size:191"`
	EntityName     string    `gorm:"size:191"`
	SignalKind     string    `gorm:"size:32;not null"` // status / handoff / comment_mention / digest
	RuleID         string    `gorm:"size:64;not null"`
	Facts          string    `gorm:"type:text"`
	Recipients     string    `gorm:"type:text"`
	Outcome        string    `gorm:"size:16;not null"` // queued / digest / suppressed
	SuppressReason string    `gorm:"size:64"`
	RefID          string    `gorm:"size:64"`
}

func (NotificationDecision) TableName() string { return "notification_decisions" }

const (
	DecisionOutcomeQueued     = "queued"
	DecisionOutcomeDigest     = "digest"
	DecisionOutcomeSuppressed = "suppressed"
)

// RecordNotificationDecision appends one decision row. Facts and recipients
// are encoded as JSON arrays/objects by the caller-friendly helpers below.
func RecordNotificationDecision(db *gorm.DB, decision NotificationDecision) error {
	if decision.CreatedAt.IsZero() {
		decision.CreatedAt = time.Now().UTC()
	}
	if err := db.Create(&decision).Error; err != nil {
		slog.Warn("RecordNotificationDecision failed",
			"err", err,
			"production_id", decision.ProductionID,
			"task_id", decision.TaskID,
			"signal_kind", decision.SignalKind,
			"rule_id", decision.RuleID,
			"outcome", decision.Outcome,
		)
		return err
	}
	return nil
}

// DecisionFactsJSON encodes a facts map for the log.
func DecisionFactsJSON(facts map[string]interface{}) string {
	if len(facts) == 0 {
		return ""
	}
	encoded, err := json.Marshal(facts)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// DecisionRecipientsJSON encodes the recipient Kitsu person IDs for the log.
func DecisionRecipientsJSON(personIDs []string) string {
	if len(personIDs) == 0 {
		return ""
	}
	encoded, err := json.Marshal(personIDs)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// NotificationDecisionRetentionDays bounds the decision log's growth.
const NotificationDecisionRetentionDays = 30

// PurgeOldNotificationDecisions deletes decision rows older than the
// retention window.
func PurgeOldNotificationDecisions(db *gorm.DB, retentionDays int) int {
	if db == nil || retentionDays <= 0 {
		return 0
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	result := db.Where("created_at < ?", cutoff).Delete(&NotificationDecision{})
	if result.Error != nil {
		return 0
	}
	return int(result.RowsAffected)
}

// StatusInfo is the minimal Kitsu status shape the reconcile needs, kept
// local so the model package does not depend on the Kitsu client.
type StatusInfo struct {
	ID   string
	Name string
}

// ReconcileStudioStatusPolicies folds the live Kitsu status list into the
// policy table: new statuses are classified by name, and manual rows are never
// re-derived. Unclassifiable statuses stay ambiguous without creating
// admin_exceptions rows; operators assign them on the タスクステータス page.
func ReconcileStudioStatusPolicies(db *gorm.DB, statuses []StatusInfo) error {
	for _, status := range statuses {
		name := strings.TrimSpace(status.Name)
		var existing StudioStatusPolicy
		err := db.Where("status_id = ?", status.ID).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			nature := ClassifyStatusName(name)
			row := StudioStatusPolicy{StatusID: status.ID, StatusName: name, Nature: nature, UpdatedAt: time.Now().UTC()}
			if err := db.Create(&row).Error; err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if existing.StatusName == name {
			continue
		}
		if existing.Manual {
			if err := db.Model(&StudioStatusPolicy{}).Where("status_id = ?", status.ID).Update("status_name", name).Error; err != nil {
				return err
			}
			continue
		}
		derived := ClassifyStatusName(name)
		if err := db.Model(&StudioStatusPolicy{}).Where("status_id = ?", status.ID).Updates(map[string]interface{}{
			"status_name": name,
			"nature":      derived,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}
