package model

import (
	"encoding/json"
	"strings"

	"gorm.io/gorm"
)

// A shelved handoff is silent by design: nobody is notified. When the same
// product type is handed off for another Task Type in the Production, the
// silence is more likely a mapping mistake, so KitsuSync records the pair for
// the admin without changing what is notified.

// SuspiciousShelf is one shelved source Task Type × product pair that another
// Task Type hands off.
type SuspiciousShelf struct {
	TaskTypeID        string `json:"task_type_id"`
	TaskTypeName      string `json:"task_type_name"`
	ProductType       string `json:"product_type"`
	OtherTaskTypeName string `json:"other_task_type_name"`
}

// HandoffShelfCheckKey is the Production's needs-review mark: one entry per
// suspicious pair, cleared by a settings save that makes the pair a handoff.
func HandoffShelfCheckKey(productionID string) string {
	return "handoff_shelve_check:" + productionID
}

// ListSuspiciousShelves returns the Production's recorded pairs.
func ListSuspiciousShelves(db *gorm.DB, productionID string) []SuspiciousShelf {
	raw := strings.TrimSpace(GetSetting(db, HandoffShelfCheckKey(productionID)))
	if raw == "" {
		return nil
	}
	var marks []SuspiciousShelf
	if err := json.Unmarshal([]byte(raw), &marks); err != nil {
		return nil
	}
	return marks
}

// RecordSuspiciousShelf stores one pair once and reports whether it was new.
func RecordSuspiciousShelf(db *gorm.DB, productionID string, mark SuspiciousShelf) (bool, error) {
	marks := ListSuspiciousShelves(db, productionID)
	for _, existing := range marks {
		if strings.TrimSpace(existing.TaskTypeID) == strings.TrimSpace(mark.TaskTypeID) &&
			strings.EqualFold(strings.TrimSpace(existing.ProductType), strings.TrimSpace(mark.ProductType)) {
			return false, nil
		}
	}
	marks = append(marks, mark)
	return true, saveSuspiciousShelves(db, productionID, marks)
}

// ReconcileSuspiciousShelves drops marks whose pair is no longer shelved.
func ReconcileSuspiciousShelves(db *gorm.DB, settings ProductionHandoffSettings) error {
	productionID := strings.TrimSpace(settings.Config.ProductionID)
	marks := ListSuspiciousShelves(db, productionID)
	if len(marks) == 0 {
		return nil
	}
	kept := make([]SuspiciousShelf, 0, len(marks))
	for _, mark := range marks {
		if shelfPairStillShelved(settings, mark.TaskTypeID, mark.ProductType) {
			kept = append(kept, mark)
		}
	}
	return saveSuspiciousShelves(db, productionID, kept)
}

// shelfPairStillShelved reports whether the pair still does nothing: a missing
// row and an explicit shelve both stay shelved.
func shelfPairStillShelved(settings ProductionHandoffSettings, taskTypeID, productType string) bool {
	for _, route := range settings.Routes {
		if strings.TrimSpace(route.TaskTypeID) != strings.TrimSpace(taskTypeID) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(route.ProductType), strings.TrimSpace(productType)) {
			continue
		}
		return route.Flow == HandoffFlowShelve
	}
	return true
}

func saveSuspiciousShelves(db *gorm.DB, productionID string, marks []SuspiciousShelf) error {
	key := HandoffShelfCheckKey(productionID)
	if len(marks) == 0 {
		return DeleteSettingWithError(db, key)
	}
	raw, err := json.Marshal(marks)
	if err != nil {
		return err
	}
	return SetSettingWithError(db, key, string(raw))
}
