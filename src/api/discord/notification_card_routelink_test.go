package discord

import (
	"testing"

	"app/src/api/kitsu"
	"app/src/utils/config"
)

func TestNotificationCardTemplateLinksAssetTasksToKitsu(t *testing.T) {
	prev := KitsuPublicURLResolver
	KitsuPublicURLResolver = func() string { return "https://kitsu.example.com" }
	t.Cleanup(func() { KitsuPublicURLResolver = prev })

	// Asset tasks carry the concrete asset-type name, not "Asset".
	elem := kitsu.MessagePayload{}
	elem.Project.Project.ID = "prod-1"
	elem.EntityType.EntityType.Name = "Character"
	elem.Entity.Entity.Name = "VB-001"
	elem.Task.ID = "task-1"

	tpl, err := NotificationCardTemplate(config.Config{}, elem, "", "ja", nil)
	if err != nil {
		t.Fatalf("NotificationCardTemplate: %v", err)
	}
	if got := tpl.TaskURL; got != "https://kitsu.example.com/productions/prod-1/assets/tasks/task-1" {
		t.Fatalf("asset task TaskURL = %q, want the Kitsu asset task route", got)
	}

	elem.EntityType.EntityType.Name = "Shot"
	elem.Task.ID = "task-2"
	tpl, err = NotificationCardTemplate(config.Config{}, elem, "", "ja", nil)
	if err != nil {
		t.Fatalf("NotificationCardTemplate (shot): %v", err)
	}
	if got := tpl.TaskURL; got != "https://kitsu.example.com/productions/prod-1/shots/tasks/task-2" {
		t.Fatalf("shot task TaskURL = %q, want the Kitsu shot task route", got)
	}
}
