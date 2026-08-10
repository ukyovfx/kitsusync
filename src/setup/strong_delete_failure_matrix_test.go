package setup

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"app/src/model"
	"gorm.io/gorm"
)

func strongDeleteValidationPass(_ string, _ model.Project, _ string, _ string, _ []model.ProjectWebhook, candidates []connectedProductionChannelCandidate) ([]connectedProductionChannelValidationResult, []connectedProductionChannelValidationResult, string, []string) {
	deletable := make([]connectedProductionChannelValidationResult, 0, len(candidates))
	for _, candidate := range candidates {
		deletable = append(deletable, connectedProductionChannelValidationResult{
			ChannelID:   candidate.ChannelID,
			StoredNames: append([]string(nil), candidate.StoredNames...),
		})
	}
	return deletable, nil, "validated", []string{"validated"}
}

func installStrongDeleteTestSeams(t *testing.T, deleteFn func(string, string) error, cleanupFn func(string, *gorm.DB) error) {
	t.Helper()
	oldValidate := strongDeleteValidateCandidates
	oldDelete := strongDeleteChannelDelete
	oldCleanup := strongDeleteConnectionCleanup
	strongDeleteValidateCandidates = strongDeleteValidationPass
	strongDeleteChannelDelete = deleteFn
	strongDeleteConnectionCleanup = cleanupFn
	t.Cleanup(func() {
		strongDeleteValidateCandidates = oldValidate
		strongDeleteChannelDelete = oldDelete
		strongDeleteConnectionCleanup = oldCleanup
	})
}

func newStrongDeleteFixture(t *testing.T, channelCount int, withRoutes bool) (*model.Project, *gorm.DB, []string) {
	t.Helper()
	db := newSetupHandlerTestDB(t)
	const productionID = "strong-delete-production"
	if err := model.CreateProject(db, productionID, "Strong Delete Production", "kitsu", "guild-1", "category-1", "en"); err != nil {
		t.Fatalf("create project: %v", err)
	}
	project := model.FindProjectByKitsuID(db, productionID)
	if project == nil {
		t.Fatal("project fixture missing")
	}
	channelIDs := make([]string, 0, channelCount)
	for i := 1; i <= channelCount; i++ {
		channelID := fmt.Sprintf("channel-%d", i)
		channelIDs = append(channelIDs, channelID)
		if err := model.CreateProjectWebhook(db, productionID, fmt.Sprintf("task-%d", i), fmt.Sprintf("task-type-%d", i), "https://discord.invalid/webhook", channelID); err != nil {
			t.Fatalf("create webhook: %v", err)
		}
	}
	webhooks := model.ListProjectWebhooks(db, productionID)
	db.Create(&model.ProductionNotificationConfig{ProductionID: productionID, ProductionName: project.Name, Enabled: true})
	for i, channelID := range channelIDs {
		db.Create(&model.ProductionChannelMapping{ProductionID: productionID, GuildID: "guild-1", TaskTypeID: fmt.Sprintf("task-type-%d", i+1), ChannelID: channelID, Active: true, State: model.ChannelMappingStateCurrent, MigrationState: model.ChannelMappingStateCurrent})
		if withRoutes {
			db.Create(&model.ProductionNotificationRoute{ProductionID: productionID, TaskTypeID: fmt.Sprintf("task-type-%d", i+1), DestinationWebhookID: webhooks[i].ID, DestinationChannelID: channelID})
		}
	}
	return project, db, channelIDs
}

func assertStrongDeleteComplete(t *testing.T, db *gorm.DB, productionID string) {
	t.Helper()
	if model.FindProjectByKitsuID(db, productionID) != nil {
		t.Fatal("project remained after terminal cleanup")
	}
	if got := model.ListProjectWebhooks(db, productionID); len(got) != 0 {
		t.Fatalf("project webhooks remained: %d", len(got))
	}
	if got := model.ListProductionChannelMappings(db, productionID); len(got) != 0 {
		t.Fatalf("channel mappings remained: %d", len(got))
	}
	if got := model.ListProductionNotificationRoutes(db, productionID); len(got) != 0 {
		t.Fatalf("notification routes remained: %d", len(got))
	}
	if model.FindProductionNotificationConfig(db, productionID) != nil {
		t.Fatal("notification config remained")
	}
}

func TestStrongDeleteFailureMatrix(t *testing.T) {
	t.Run("channel failure remains diagnosable and does not leak upstream error", func(t *testing.T) {
		project, db, channelIDs := newStrongDeleteFixture(t, 2, true)
		secretErr := fmt.Errorf("response body webhook https://example.invalid/api/webhooks/123/secret-token Authorization: Bot token-like-value")
		installStrongDeleteTestSeams(t, func(channelID, _ string) error {
			if channelID == channelIDs[1] {
				return secretErr
			}
			return nil
		}, func(productionID string, db *gorm.DB) error {
			return DeleteProjectConnectionOnly(productionID, db)
		})
		result := executeConnectedProductionStrongDelete("en", *project, "guild-1", "bot-token", db)
		if len(result.Failed) != 1 || result.ConnectionDeleted != true || result.CategoryDeleted != true {
			t.Fatalf("unexpected channel failure result: %+v", result)
		}
		if strings.Contains(result.Failed[0].Reason, "secret-token") || strings.Contains(result.Failed[0].Reason, "Authorization") {
			t.Fatalf("upstream error leaked: %q", result.Failed[0].Reason)
		}
		assertStrongDeleteComplete(t, db, project.KitsuProjectID)
	})

	t.Run("category failure preserves connection and reports warning", func(t *testing.T) {
		project, db, _ := newStrongDeleteFixture(t, 2, true)
		installStrongDeleteTestSeams(t, func(channelID, _ string) error {
			if channelID == project.DiscordCategoryID {
				return fmt.Errorf("category delete failed: response body secret-token")
			}
			return nil
		}, func(productionID string, db *gorm.DB) error {
			return DeleteProjectConnectionOnly(productionID, db)
		})
		result := executeConnectedProductionStrongDelete("en", *project, "guild-1", "bot-token", db)
		if result.ConnectionDeleted || result.CategoryError == "" || result.CategoryDeleted {
			t.Fatalf("category failure was treated as success: %+v", result)
		}
		if model.FindProjectByKitsuID(db, project.KitsuProjectID) == nil {
			t.Fatal("project was removed despite category failure")
		}
		if strings.Contains(result.CategoryError, "secret-token") {
			t.Fatalf("category error leaked upstream detail: %q", result.CategoryError)
		}
	})

	t.Run("local cleanup failure is not reported as complete", func(t *testing.T) {
		project, db, _ := newStrongDeleteFixture(t, 1, true)
		installStrongDeleteTestSeams(t, func(string, string) error { return nil }, func(string, *gorm.DB) error {
			return fmt.Errorf("sqlite failed: webhook secret-token")
		})
		result := executeConnectedProductionStrongDelete("en", *project, "guild-1", "bot-token", db)
		if result.ConnectionDeleted || result.ConnectionError == "" {
			t.Fatalf("local cleanup failure was treated as complete: %+v", result)
		}
		if strings.Contains(result.ConnectionError, "secret-token") {
			t.Fatalf("local cleanup error leaked upstream detail: %q", result.ConnectionError)
		}
	})

	t.Run("category 404 is an accepted terminal state", func(t *testing.T) {
		project, db, _ := newStrongDeleteFixture(t, 1, true)
		installStrongDeleteTestSeams(t, func(channelID, _ string) error {
			if channelID == project.DiscordCategoryID {
				return discordBotAPIError("category delete failed", http.StatusNotFound, []byte(`{"message":"Unknown Channel"}`))
			}
			return nil
		}, func(productionID string, db *gorm.DB) error {
			return DeleteProjectConnectionOnly(productionID, db)
		})
		result := executeConnectedProductionStrongDelete("en", *project, "guild-1", "bot-token", db)
		if !result.CategoryDeleted || !result.ConnectionDeleted || result.CategoryError != "" {
			t.Fatalf("category 404 was not idempotent: %+v", result)
		}
		assertStrongDeleteComplete(t, db, project.KitsuProjectID)
	})

	t.Run("already absent webhook is idempotent", func(t *testing.T) {
		project, db, _ := newStrongDeleteFixture(t, 0, false)
		installStrongDeleteTestSeams(t, func(string, string) error { return nil }, func(productionID string, db *gorm.DB) error {
			return DeleteProjectConnectionOnly(productionID, db)
		})
		result := executeConnectedProductionStrongDelete("en", *project, "guild-1", "bot-token", db)
		if !result.ConnectionDeleted || !result.CategoryDeleted {
			t.Fatalf("already absent webhook was not idempotent: %+v", result)
		}
		assertStrongDeleteComplete(t, db, project.KitsuProjectID)
	})

	t.Run("already absent route is idempotent", func(t *testing.T) {
		project, db, _ := newStrongDeleteFixture(t, 1, false)
		installStrongDeleteTestSeams(t, func(string, string) error { return nil }, func(productionID string, db *gorm.DB) error {
			return DeleteProjectConnectionOnly(productionID, db)
		})
		result := executeConnectedProductionStrongDelete("en", *project, "guild-1", "bot-token", db)
		if !result.ConnectionDeleted || !result.CategoryDeleted {
			t.Fatalf("already absent route was not idempotent: %+v", result)
		}
		assertStrongDeleteComplete(t, db, project.KitsuProjectID)
	})
}

func TestStrongDeleteCombinedFailureConvergesOnRetry(t *testing.T) {
	project, db, channelIDs := newStrongDeleteFixture(t, 2, true)
	categoryFailed := true
	channelFailed := true
	installStrongDeleteTestSeams(t, func(channelID, _ string) error {
		if channelID == channelIDs[1] && channelFailed {
			return fmt.Errorf("channel unavailable")
		}
		if channelID == project.DiscordCategoryID && categoryFailed {
			return fmt.Errorf("category unavailable")
		}
		return nil
	}, func(productionID string, db *gorm.DB) error {
		return DeleteProjectConnectionOnly(productionID, db)
	})

	first := executeConnectedProductionStrongDelete("en", *project, "guild-1", "bot-token", db)
	if len(first.Failed) != 1 || first.ConnectionDeleted || first.CategoryError == "" {
		t.Fatalf("combined failure did not remain partial: %+v", first)
	}
	categoryFailed = false
	channelFailed = false
	second := executeConnectedProductionStrongDelete("en", *project, "guild-1", "bot-token", db)
	if len(second.Failed) != 0 || !second.ConnectionDeleted || !second.CategoryDeleted {
		t.Fatalf("retry did not converge: %+v", second)
	}
	assertStrongDeleteComplete(t, db, project.KitsuProjectID)
}
