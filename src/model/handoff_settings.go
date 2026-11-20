package model

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Handoff flows a source Task Type's product can take on delivery.
const (
	HandoffFlowSubmit = "submit" // 提出
	HandoffFlowHand   = "hand"   // 受け渡し
	HandoffFlowShelve = "shelve" // 棚置き
)

func validHandoffFlow(flow string) bool {
	switch flow {
	case HandoffFlowSubmit, HandoffFlowHand, HandoffFlowShelve:
		return true
	}
	return false
}

// Notification timing and the recipient used when a notify Task Type has no
// assignee.
const (
	HandoffTimingImmediate = "immediate"
	HandoffTimingDigest    = "digest"
	// HandoffTimingMixed is the owner's rule: a handoff whose receiving task
	// has an assignee is sent immediately, one that fell back is told in the
	// morning digest. It is also what an unconfigured Production gets.
	HandoffTimingMixed = "mixed"

	HandoffFallbackAdmin    = "admin"
	HandoffFallbackUsers    = "users"
	HandoffFallbackDisabled = "none"
	// HandoffFallbackNone marks a handoff whose receiving task had an
	// assignee, so no fallback was used.
	HandoffFallbackNone = "none"
)

// ValidHandoffTiming reports whether a notification timing is one of the three
// the settings page offers.
func ValidHandoffTiming(timing string) bool {
	return timing == HandoffTimingImmediate || timing == HandoffTimingDigest || timing == HandoffTimingMixed
}

func validHandoffTiming(timing string) bool {
	return ValidHandoffTiming(timing)
}

func validHandoffFallback(fallback string) bool {
	switch fallback {
	case HandoffFallbackAdmin, HandoffFallbackUsers, HandoffFallbackDisabled:
		return true
	}
	return false
}

// ProductionHandoffConfig is one Production's material handoff configuration.
// Product types are a per-Production list of names, so a Production without any
// product type has no rows and the handoff path stays inactive.
type ProductionHandoffConfig struct {
	ID             uint   `gorm:"primaryKey"`
	ProductionID   string `gorm:"uniqueIndex;not null"`
	NotifyTiming   string `gorm:"not null"`
	NoAssignee     string `gorm:"not null"`
	FallbackUserID string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ProductionHandoffProductType is one registered product type name. The name is
// the identity: the mapping table is keyed by Task Type × product type name.
type ProductionHandoffProductType struct {
	ID           uint   `gorm:"primaryKey"`
	ProductionID string `gorm:"not null;uniqueIndex:idx_handoff_product_type"`
	Name         string `gorm:"not null;uniqueIndex:idx_handoff_product_type"`
	// Unclassified marks a product type the detector registered on its own
	// when a publish arrived with a name the mapping table did not know. It
	// starts shelved (no notification) until the operator classifies it.
	Unclassified bool `gorm:"not null;default:false"`
	CreatedAt    time.Time
}

// ProductionHandoffRoute is one row of the mapping table: the flow a source
// Task Type's product takes, plus the Task Types notified on delivery.
type ProductionHandoffRoute struct {
	ID                 uint   `gorm:"primaryKey"`
	ProductionID       string `gorm:"not null;uniqueIndex:idx_handoff_route"`
	TaskTypeID         string `gorm:"not null;uniqueIndex:idx_handoff_route"`
	TaskTypeName       string
	ProductType        string `gorm:"not null;uniqueIndex:idx_handoff_route"`
	Flow               string `gorm:"not null"`
	NotifyTaskTypeIDs  string // comma separated Task Type IDs
	NotifyTaskTypeName string // comma separated display names, same order
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// ProductionHandoffSettings is one Production's complete handoff configuration.
type ProductionHandoffSettings struct {
	Config       ProductionHandoffConfig
	ProductTypes []ProductionHandoffProductType
	Routes       []ProductionHandoffRoute
}

// LoadProductionHandoffSettings reads a Production's handoff configuration. ok
// is false when the Production has no handoff configuration row, which is what
// keeps the handoff path inactive for unconfigured Productions.
func LoadProductionHandoffSettings(db *gorm.DB, productionID string) (settings ProductionHandoffSettings, ok bool, err error) {
	if db == nil || strings.TrimSpace(productionID) == "" {
		return settings, false, gorm.ErrInvalidData
	}
	productionID = strings.TrimSpace(productionID)
	if err := db.Where("production_id = ?", productionID).First(&settings.Config).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return settings, false, nil
		}
		return settings, false, err
	}
	if err := db.Where("production_id = ?", productionID).Order("id asc").Find(&settings.ProductTypes).Error; err != nil {
		return settings, true, err
	}
	if err := db.Where("production_id = ?", productionID).Order("id asc").Find(&settings.Routes).Error; err != nil {
		return settings, true, err
	}
	return settings, true, nil
}

// HandoffSettingsProblem is one validation failure as a code plus arguments.
type HandoffSettingsProblem struct {
	Code string
	Args []string
}

// English renders the problem as a plain English sentence.
func (p HandoffSettingsProblem) English() string {
	arg := func(i int) string {
		if i < len(p.Args) {
			return p.Args[i]
		}
		return ""
	}
	switch p.Code {
	case ProblemHandoffProductionRequired:
		return "production ID is required"
	case ProblemHandoffProductTypeRequired:
		return "a product type name is required"
	case ProblemHandoffProductTypeDuplicate:
		return fmt.Sprintf("product type %q is registered more than once", arg(0))
	case ProblemHandoffRouteIncomplete:
		return "every handoff row needs a Task Type and a product type"
	case ProblemHandoffRouteProductUnknown:
		return fmt.Sprintf("Task Type %s uses product type %q, which is not registered", arg(0), arg(1))
	case ProblemHandoffRouteDuplicate:
		return fmt.Sprintf("Task Type %s with product type %q is mapped more than once", arg(0), arg(1))
	case ProblemHandoffFlowInvalid:
		return fmt.Sprintf("Task Type %s with product type %q has no valid flow", arg(0), arg(1))
	case ProblemHandoffNotifyInvalid:
		return fmt.Sprintf("Task Type %s with product type %q notifies an unknown Task Type %s", arg(0), arg(1), arg(2))
	case ProblemHandoffTimingInvalid:
		return fmt.Sprintf("notification timing %q is not valid", arg(0))
	case ProblemHandoffFallbackInvalid:
		return fmt.Sprintf("no-assignee handling %q is not valid", arg(0))
	case ProblemHandoffFallbackUserRequired:
		return "choosing a user for no-assignee notifications needs that user's ID"
	}
	return p.Code
}

// Problem codes of HandoffSettingsProblem.
const (
	ProblemHandoffProductionRequired   = "handoff_production_required"
	ProblemHandoffProductTypeRequired  = "handoff_product_type_required"
	ProblemHandoffProductTypeDuplicate = "handoff_product_type_duplicate"
	ProblemHandoffRouteIncomplete      = "handoff_route_incomplete"
	ProblemHandoffRouteProductUnknown  = "handoff_route_product_unknown"
	ProblemHandoffRouteDuplicate       = "handoff_route_duplicate"
	ProblemHandoffFlowInvalid          = "handoff_flow_invalid"
	ProblemHandoffNotifyInvalid        = "handoff_notify_invalid"
	ProblemHandoffTimingInvalid        = "handoff_timing_invalid"
	ProblemHandoffFallbackInvalid      = "handoff_fallback_invalid"
	ProblemHandoffFallbackUserRequired = "handoff_fallback_user_required"
)

// HandoffSettingsError carries the structured problems of a rejected save.
type HandoffSettingsError struct {
	Problems []HandoffSettingsProblem
}

func (e *HandoffSettingsError) Error() string {
	parts := make([]string, 0, len(e.Problems))
	for _, problem := range e.Problems {
		parts = append(parts, problem.English())
	}
	return "invalid handoff settings: " + strings.Join(parts, "; ")
}

// HandoffSettingsProblems checks the configuration without touching Kitsu.
func HandoffSettingsProblems(settings ProductionHandoffSettings) []HandoffSettingsProblem {
	var problems []HandoffSettingsProblem
	add := func(code string, args ...string) {
		problems = append(problems, HandoffSettingsProblem{Code: code, Args: args})
	}
	if strings.TrimSpace(settings.Config.ProductionID) == "" {
		add(ProblemHandoffProductionRequired)
	}
	if !validHandoffTiming(settings.Config.NotifyTiming) {
		add(ProblemHandoffTimingInvalid, settings.Config.NotifyTiming)
	}
	if !validHandoffFallback(settings.Config.NoAssignee) {
		add(ProblemHandoffFallbackInvalid, settings.Config.NoAssignee)
	}
	if settings.Config.NoAssignee == HandoffFallbackUsers && strings.TrimSpace(settings.Config.FallbackUserID) == "" {
		add(ProblemHandoffFallbackUserRequired)
	}

	products := map[string]struct{}{}
	for _, product := range settings.ProductTypes {
		name := strings.TrimSpace(product.Name)
		if name == "" {
			add(ProblemHandoffProductTypeRequired)
			continue
		}
		if _, dup := products[name]; dup {
			add(ProblemHandoffProductTypeDuplicate, name)
		}
		products[name] = struct{}{}
	}

	rows := map[string]struct{}{}
	for _, route := range settings.Routes {
		taskTypeID := strings.TrimSpace(route.TaskTypeID)
		product := strings.TrimSpace(route.ProductType)
		if taskTypeID == "" || product == "" {
			add(ProblemHandoffRouteIncomplete)
			continue
		}
		if _, known := products[product]; !known {
			add(ProblemHandoffRouteProductUnknown, taskTypeID, product)
		}
		key := taskTypeID + "\x00" + product
		if _, dup := rows[key]; dup {
			add(ProblemHandoffRouteDuplicate, taskTypeID, product)
		}
		rows[key] = struct{}{}
		if !validHandoffFlow(route.Flow) {
			add(ProblemHandoffFlowInvalid, taskTypeID, product)
		}
		for _, notify := range SplitHandoffNotifyIDs(route.NotifyTaskTypeIDs) {
			if notify == "" {
				add(ProblemHandoffNotifyInvalid, taskTypeID, product, notify)
			}
		}
	}
	return problems
}

// SplitHandoffNotifyIDs splits the stored comma separated notify list.
func SplitHandoffNotifyIDs(raw string) []string {
	var ids []string
	for _, part := range strings.Split(raw, ",") {
		if id := strings.TrimSpace(part); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// JoinHandoffNotifyIDs stores the notify list in one column, which keeps the row
// a single record instead of a second table.
func JoinHandoffNotifyIDs(ids []string) string {
	var parts []string
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			parts = append(parts, id)
		}
	}
	return strings.Join(parts, ",")
}

// SaveProductionHandoffSettings validates and replaces a Production's whole
// handoff configuration in one transaction.
func SaveProductionHandoffSettings(db *gorm.DB, settings ProductionHandoffSettings) error {
	if db == nil {
		return gorm.ErrInvalidDB
	}
	settings.Config.ProductionID = strings.TrimSpace(settings.Config.ProductionID)
	if problems := HandoffSettingsProblems(settings); len(problems) > 0 {
		return &HandoffSettingsError{Problems: problems}
	}
	productionID := settings.Config.ProductionID
	return db.Transaction(func(tx *gorm.DB) error {
		var existing ProductionHandoffConfig
		if err := tx.Where("production_id = ?", productionID).First(&existing).Error; err == nil {
			settings.Config.ID = existing.ID
			settings.Config.CreatedAt = existing.CreatedAt
		} else if err != gorm.ErrRecordNotFound {
			return err
		}
		settings.Config.ID = existing.ID
		if err := tx.Save(&settings.Config).Error; err != nil {
			return err
		}
		if err := deleteProductionHandoffChildren(tx, productionID); err != nil {
			return err
		}
		for i := range settings.ProductTypes {
			settings.ProductTypes[i].ID = 0
			settings.ProductTypes[i].ProductionID = productionID
			settings.ProductTypes[i].Name = strings.TrimSpace(settings.ProductTypes[i].Name)
			if err := tx.Create(&settings.ProductTypes[i]).Error; err != nil {
				return err
			}
		}
		for i := range settings.Routes {
			settings.Routes[i].ID = 0
			settings.Routes[i].ProductionID = productionID
			settings.Routes[i].NotifyTaskTypeIDs = JoinHandoffNotifyIDs(SplitHandoffNotifyIDs(settings.Routes[i].NotifyTaskTypeIDs))
			if err := tx.Create(&settings.Routes[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// RegisterUnclassifiedHandoffProductType adds a product type the detector met
// for the first time to the Production's list as unclassified, and reports
// whether it was new. A name already registered (case-insensitively) is left
// alone, so a repeated publish never registers it twice.
func RegisterUnclassifiedHandoffProductType(db *gorm.DB, productionID, name string) (bool, error) {
	productionID, name = strings.TrimSpace(productionID), strings.TrimSpace(name)
	if db == nil || productionID == "" || name == "" {
		return false, gorm.ErrInvalidData
	}
	created := false
	err := db.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&ProductionHandoffProductType{}).Where("production_id = ? AND lower(name) = lower(?)", productionID, name).Count(&count).Error; err != nil || count > 0 {
			return err
		}
		if err := tx.Create(&ProductionHandoffProductType{ProductionID: productionID, Name: name, Unclassified: true}).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	return created, err
}

// DeleteProductionHandoffSettings removes a Production's handoff configuration.
func DeleteProductionHandoffSettings(db *gorm.DB, productionID string) error {
	if db == nil || strings.TrimSpace(productionID) == "" {
		return gorm.ErrInvalidData
	}
	productionID = strings.TrimSpace(productionID)
	return db.Transaction(func(tx *gorm.DB) error {
		if err := deleteProductionHandoffChildren(tx, productionID); err != nil {
			return err
		}
		return tx.Where("production_id = ?", productionID).Delete(&ProductionHandoffConfig{}).Error
	})
}

func deleteProductionHandoffChildren(tx *gorm.DB, productionID string) error {
	for _, table := range []any{&ProductionHandoffProductType{}, &ProductionHandoffRoute{}} {
		if err := tx.Where("production_id = ?", productionID).Delete(table).Error; err != nil {
			return err
		}
	}
	return nil
}
