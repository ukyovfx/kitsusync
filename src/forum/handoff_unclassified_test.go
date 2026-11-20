package forum

import (
	"testing"

	"app/src/api/kitsu"
	"app/src/model"
)

func vdbDelivery(commentID string) []kitsu.MessagePayload {
	table := "|  |  |\n|--|--|\n| version | 1 |\n| family | vdb |\n| name | sq010_sh001_vdb |\n"
	return []kitsu.MessagePayload{
		deliveryPayload("task-src", "tt-mm", commentID, table),
		receiverPayload("tt-comp", "person-comp"),
	}
}

// A product type the table has never heard of is registered once as
// unclassified and notifies nobody.
func TestHandoffDetectRegistersUnknownProductTypeAsUnclassified(t *testing.T) {
	db := newHandoffDB(t)
	seedHandoffRoutes(t, db, model.HandoffFlowHand, model.HandoffFallbackAdmin)

	for i, commentID := range []string{"comment-1", "comment-2"} {
		result, err := (HandoffDetector{DB: db}).Detect(vdbDelivery(commentID))
		if err != nil {
			t.Fatalf("detect: %v", err)
		}
		if result.Events != 0 {
			t.Fatalf("an unclassified product must not notify, got %d events", result.Events)
		}
		if want := map[int]int{0: 1, 1: 0}[i]; result.Unclassified != want {
			t.Fatalf("pass %d registered %d, want %d", i, result.Unclassified, want)
		}
	}

	settings, _, err := model.LoadProductionHandoffSettings(db, "prod-1")
	if err != nil {
		t.Fatal(err)
	}
	var vdb []model.ProductionHandoffProductType
	for _, product := range settings.ProductTypes {
		if product.Name == "vdb" {
			vdb = append(vdb, product)
		}
	}
	if len(vdb) != 1 || !vdb[0].Unclassified {
		t.Fatalf("vdb registration = %+v, want exactly one unclassified row", vdb)
	}
	// The product the operator configured is untouched and still classified.
	for _, product := range settings.ProductTypes {
		if product.Name == "matchmove" && product.Unclassified {
			t.Fatal("a configured product type was marked unclassified")
		}
	}
	events, err := model.PendingHandoffEvents(db)
	if err != nil || len(events) != 0 {
		t.Fatalf("pending events = %v err=%v", events, err)
	}
}

// Registration is case-insensitive and in one pass registers a family once.
func TestHandoffDetectUnknownProductTypeRegisteredOncePerPass(t *testing.T) {
	db := newHandoffDB(t)
	seedHandoffRoutes(t, db, model.HandoffFlowHand, model.HandoffFallbackAdmin)
	payloads := append(vdbDelivery("comment-1"), deliveryPayload("task-src2", "tt-mm", "comment-2", "|  |  |\n|--|--|\n| version | 2 |\n| family | VDB |\n| name | sq010_sh002_vdb |\n"))

	result, err := (HandoffDetector{DB: db}).Detect(payloads)
	if err != nil {
		t.Fatal(err)
	}
	if result.Unclassified != 1 {
		t.Fatalf("registered %d, want 1", result.Unclassified)
	}
	var count int64
	db.Model(&model.ProductionHandoffProductType{}).Where("production_id = ? AND lower(name) = ?", "prod-1", "vdb").Count(&count)
	if count != 1 {
		t.Fatalf("vdb rows = %d, want 1", count)
	}
}

// A Production without handoff settings stays fully inactive.
func TestHandoffDetectDoesNotRegisterForUnconfiguredProduction(t *testing.T) {
	db := newHandoffDB(t)
	result, err := (HandoffDetector{DB: db}).Detect(vdbDelivery("comment-1"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Unclassified != 0 {
		t.Fatal("registered a product type for an unconfigured Production")
	}
	if _, ok, _ := model.LoadProductionHandoffSettings(db, "prod-1"); ok {
		t.Fatal("handoff settings were created by detection")
	}
}
