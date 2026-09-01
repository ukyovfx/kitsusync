package main

import (
	"app/src/api/kitsu"
	"app/src/model"
	"testing"
)

func TestAuditActorFromPayloadUsesProvenCommentAuthor(t *testing.T) {
	var payload kitsu.MessagePayload
	payload.IsCommentOnly = true
	payload.LatestComment.Author.ID = "person-1"
	payload.LatestComment.Author.FullName = "Ukyo Matsuo"
	if kind, id, name := auditActorFromPayload(payload); kind != model.AuditActorHuman || id != "person-1" || name != "Ukyo Matsuo" {
		t.Fatalf("actor = %q/%q/%q, want human/person-1/Ukyo Matsuo", kind, id, name)
	}
}

func TestAuditActorFromPayloadClassifiesSystemAndUnknownConservatively(t *testing.T) {
	var botPayload kitsu.MessagePayload
	botPayload.IsCommentOnly = true
	botPayload.LatestComment.Author.ID = "bot-1"
	botPayload.LatestComment.Author.IsBot = true
	if kind, id, name := auditActorFromPayload(botPayload); kind != model.AuditActorSystem || id != "bot-1" || name != "" {
		t.Fatalf("bot actor = %q/%q/%q, want system/bot-1/empty", kind, id, name)
	}

	var statusPayload kitsu.MessagePayload
	statusPayload.LatestComment.Author.ID = "person-1"
	statusPayload.LatestComment.Author.FullName = "Wrong attribution"
	if kind, id, name := auditActorFromPayload(statusPayload); kind != model.AuditActorUnknown || id != "" || name != "" {
		t.Fatalf("status actor = %q/%q/%q, want unknown/empty/empty", kind, id, name)
	}
}
