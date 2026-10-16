package vfxfiles

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"app/src/model"
	"gorm.io/gorm"
)

const maxEventBatchBytes = 1 << 20

var eventSignatureRE = regexp.MustCompile(`^sha256=[a-f0-9]{64}$`)

type eventBatch struct {
	ContractVersion string            `json:"contract_version"`
	Events          []json.RawMessage `json:"events"`
}
type pluginEvent struct {
	ContractVersion         string               `json:"contract_version"`
	ProducerID              string               `json:"producer_id"`
	EventID                 string               `json:"event_id"`
	EventType               string               `json:"event_type"`
	OccurredAt              string               `json:"occurred_at"`
	ProductionID            string               `json:"production_id"`
	ActivationEpoch         uint64               `json:"activation_epoch"`
	ConfigRevision          uint64               `json:"config_revision,omitempty"`
	PreviousActivationEpoch uint64               `json:"previous_activation_epoch,omitempty"`
	NewActivationEpoch      uint64               `json:"new_activation_epoch,omitempty"`
	ProductionName          string               `json:"production_name,omitempty"`
	EpisodeID               *string              `json:"episode_id,omitempty"`
	EpisodeName             *string              `json:"episode_name,omitempty"`
	Entity                  *eventEntity         `json:"entity,omitempty"`
	Source                  *eventTask           `json:"source,omitempty"`
	Sender                  *eventPerson         `json:"sender,omitempty"`
	SenderPersonID          *string              `json:"sender_person_id,omitempty"`
	OutputPackage           *eventOutputPackage  `json:"output_package,omitempty"`
	Delivery                *eventDelivery       `json:"delivery,omitempty"`
	Folder                  *eventFolder         `json:"folder,omitempty"`
	Recipient               *eventShareRecipient `json:"recipient,omitempty"`
	Share                   *eventShare          `json:"share,omitempty"`
}

type eventEntity struct {
	Type string  `json:"type"`
	ID   string  `json:"id"`
	Name string  `json:"name"`
	Code *string `json:"code,omitempty"`
}
type eventTask struct {
	ID           string `json:"id,omitempty"`
	TaskID       string `json:"task_id,omitempty"`
	TaskTypeID   string `json:"task_type_id"`
	TaskTypeName string `json:"task_type_name,omitempty"`
	EntityType   string `json:"entity_type,omitempty"`
	EntityID     string `json:"entity_id,omitempty"`
	EntityName   string `json:"entity_name,omitempty"`
}
type eventPerson struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}
type eventOutputPackage struct {
	ID             string           `json:"id"`
	Version        int              `json:"version"`
	Classification string           `json:"classification"`
	ManifestSHA256 string           `json:"manifest_sha256"`
	ManifestRef    *string          `json:"manifest_ref,omitempty"`
	Objects        []eventObject    `json:"objects"`
	FrameRange     *eventFrameRange `json:"frame_range,omitempty"`
}
type eventObject struct {
	DriveFileID    string `json:"drive_file_id"`
	Classification string `json:"classification"`
	SizeBytes      int64  `json:"size_bytes"`
	SHA256         string `json:"sha256"`
}
type eventFrameRange struct {
	Start   int `json:"start"`
	End     int `json:"end"`
	Step    int `json:"step"`
	RateNum int `json:"rate_num"`
	RateDen int `json:"rate_den"`
}
type eventDelivery struct {
	ID                    string  `json:"id"`
	Revision              uint64  `json:"revision"`
	Status                string  `json:"status"`
	RecipientTaskID       string  `json:"recipient_task_id"`
	RecipientTaskTypeID   string  `json:"recipient_task_type_id"`
	RecipientTaskTypeName string  `json:"recipient_task_type_name"`
	FeedbackCode          *string `json:"feedback_code,omitempty"`
}
type eventFolder struct {
	ObjectType   string `json:"object_type"`
	ObjectID     string `json:"object_id"`
	DriveFileID  string `json:"drive_file_id"`
	BindingState string `json:"binding_state"`
}

type eventShareRecipient struct {
	TaskID         string  `json:"task_id"`
	TaskTypeID     string  `json:"task_type_id"`
	TaskTypeName   *string `json:"task_type_name"`
	DepartmentID   *string `json:"department_id"`
	DepartmentName *string `json:"department_name"`
}
type eventShare struct {
	ID             string              `json:"id"`
	ShareTargetID  string              `json:"share_target_id"`
	Revision       uint64              `json:"revision"`
	SharedByID     string              `json:"shared_by_id"`
	SharedAt       string              `json:"shared_at"`
	IdempotencyKey string              `json:"idempotency_key"`
	Object         eventShareObject    `json:"object"`
	Metadata       eventShareMetadata  `json:"metadata"`
	Inventory      eventShareInventory `json:"inventory"`
	Access         eventShareAccess    `json:"access"`
}
type eventShareObject struct {
	Provider   string `json:"provider"`
	ObjectID   string `json:"object_id"`
	ObjectType string `json:"object_type"`
	URL        string `json:"url"`
}
type eventShareMetadata struct {
	Status     string  `json:"status"`
	ObservedAt string  `json:"observed_at"`
	Name       *string `json:"name"`
	MIMEType   *string `json:"mime_type"`
	SizeBytes  *int64  `json:"size_bytes"`
	ModifiedAt *string `json:"modified_at"`
	SHA256     *string `json:"sha256"`
}
type eventShareInventory struct {
	Status                      string          `json:"status"`
	ObservedAt                  string          `json:"observed_at"`
	Scope                       string          `json:"scope"`
	FileCount                   *int64          `json:"file_count"`
	ObservedFileCountLowerBound *int64          `json:"observed_file_count_lower_bound"`
	FrameSummary                json.RawMessage `json:"frame_summary"`
}
type eventShareAccess struct {
	Status           string  `json:"status"`
	AccessVerifiedAt *string `json:"access_verified_at"`
	CheckSubjectType string  `json:"check_subject_type"`
	CheckSubjectID   *string `json:"check_subject_id"`
}

// FileDeliveryNotification is the bounded display subset consumed by the
// existing Discord delivery path. It intentionally excludes Drive URLs and
// object IDs; the Plugin contract does not grant public sharing access.
type FileDeliveryNotification struct {
	EventID, EventType, ProductionID, ProductionName, OccurredAt            string
	EntityType, EntityID, EntityName                                        string
	SourceTaskID, SourceTaskTypeID, SourceTaskTypeName                      string
	SenderName, OutputClassification                                        string
	OutputVersion                                                           int
	DeliveryID, RecipientTaskID, RecipientTaskTypeID, RecipientTaskTypeName string
}

// FileShareNotification is the strict, bounded Discord display subset for
// contract 1.1 link shares. It is never built from producer consent fields.
type FileShareNotification struct {
	EventID, EventType, ProductionID, OccurredAt                string
	EntityType, EntityID, EntityName                            string
	SourceTaskTypeID, SourceTaskTypeName                        string
	RecipientTaskID, RecipientTaskTypeID, RecipientTaskTypeName string
	ObjectID, ObjectType, URL, Name                             string
	ShareTargetID                                               string
}

func DecodeFileShareNotification(raw []byte) (FileShareNotification, error) {
	input, err := parsePluginEvent(raw)
	if err != nil || input.EventType != "file_share.created" {
		return FileShareNotification{}, errors.New("invalid file share event")
	}
	var event pluginEvent
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&event) != nil || ensureSingleJSONValue(d) != nil || event.Source == nil || event.Recipient == nil || event.Share == nil {
		return FileShareNotification{}, errors.New("invalid file share event")
	}
	name := ""
	if event.Share.Metadata.Name != nil {
		name = *event.Share.Metadata.Name
	}
	taskTypeName := ""
	if event.Recipient.TaskTypeName != nil {
		taskTypeName = *event.Recipient.TaskTypeName
	}
	return FileShareNotification{EventID: event.EventID, EventType: event.EventType, ProductionID: event.ProductionID, OccurredAt: event.OccurredAt,
		EntityType: event.Source.EntityType, EntityID: event.Source.EntityID, EntityName: event.Source.EntityName,
		SourceTaskTypeID: event.Source.TaskTypeID, SourceTaskTypeName: event.Source.TaskTypeName,
		RecipientTaskID: event.Recipient.TaskID, RecipientTaskTypeID: event.Recipient.TaskTypeID, RecipientTaskTypeName: taskTypeName,
		ObjectID: event.Share.Object.ObjectID, ObjectType: event.Share.Object.ObjectType, URL: event.Share.Object.URL, Name: name, ShareTargetID: event.Share.ShareTargetID}, nil
}

func textOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// DecodeFileDeliveryNotification validates a durable inbox payload before it
// is used to build a user-visible notification.
func DecodeFileDeliveryNotification(raw []byte) (FileDeliveryNotification, error) {
	input, err := parsePluginEvent(raw)
	if err != nil || !strings.HasPrefix(input.EventType, "file_delivery.") {
		return FileDeliveryNotification{}, errors.New("invalid file delivery event")
	}
	var event pluginEvent
	if err := json.Unmarshal(raw, &event); err != nil || event.Entity == nil || event.Source == nil || event.Sender == nil || event.OutputPackage == nil || event.Delivery == nil {
		return FileDeliveryNotification{}, errors.New("invalid file delivery event")
	}
	return FileDeliveryNotification{
		EventID: event.EventID, EventType: event.EventType, ProductionID: event.ProductionID, ProductionName: event.ProductionName, OccurredAt: event.OccurredAt,
		EntityType: event.Entity.Type, EntityID: event.Entity.ID, EntityName: event.Entity.Name,
		SourceTaskID: event.Source.ID, SourceTaskTypeID: event.Source.TaskTypeID, SourceTaskTypeName: event.Source.TaskTypeName,
		SenderName: event.Sender.DisplayName, OutputClassification: event.OutputPackage.Classification, OutputVersion: event.OutputPackage.Version,
		DeliveryID: event.Delivery.ID, RecipientTaskID: event.Delivery.RecipientTaskID, RecipientTaskTypeID: event.Delivery.RecipientTaskTypeID,
		RecipientTaskTypeName: event.Delivery.RecipientTaskTypeName,
	}, nil
}

// EventHandler verifies the exact signed raw body and durably records nonce and
// event identity before returning 202. The supplied keys are server-only and
// are bound to the sole contract producer (vfx-files-plugin).
func EventHandler(db *gorm.DB, keys map[string][]byte, now func() time.Time) http.HandlerFunc {
	if now == nil {
		now = time.Now
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if db == nil || len(keys) == 0 {
			http.Error(w, "integration unavailable", http.StatusServiceUnavailable)
			return
		}
		if mediaType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])); mediaType != "application/json" {
			http.Error(w, "invalid content type", http.StatusUnsupportedMediaType)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxEventBatchBytes+1))
		if err != nil || len(body) == 0 || len(body) > maxEventBatchBytes {
			http.Error(w, "invalid event batch", http.StatusRequestEntityTooLarge)
			return
		}
		keyID := strings.TrimSpace(r.Header.Get("X-VFX-Key-Id"))
		timestamp := strings.TrimSpace(r.Header.Get("X-VFX-Timestamp"))
		nonce := strings.TrimSpace(r.Header.Get("X-VFX-Nonce"))
		suppliedHash := strings.TrimSpace(r.Header.Get("X-VFX-Content-SHA256"))
		signature := strings.TrimSpace(r.Header.Get("X-VFX-Signature"))
		idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		key := keys[keyID]
		if !keyIDRE.MatchString(keyID) || len(key) < 32 || len(nonce) < 16 || len(nonce) > 128 || strings.ContainsAny(nonce, "\r\n") || len(idempotencyKey) < 16 || len(idempotencyKey) > 128 || strings.ContainsAny(idempotencyKey, "\r\n") || !eventSignatureRE.MatchString(signature) {
			http.Error(w, "invalid event authentication", http.StatusUnauthorized)
			return
		}
		parsedTime, err := time.Parse(time.RFC3339, timestamp)
		current := now().UTC()
		if err != nil || parsedTime.Before(current.Add(-5*time.Minute)) || parsedTime.After(current.Add(5*time.Minute)) {
			http.Error(w, "invalid event timestamp", http.StatusUnauthorized)
			return
		}
		sum := sha256.Sum256(body)
		bodyHash := hex.EncodeToString(sum[:])
		if suppliedHash != bodyHash {
			http.Error(w, "event body hash mismatch", http.StatusUnauthorized)
			return
		}
		canonical := timestamp + "\n" + nonce + "\n" + bodyHash + "\n" + idempotencyKey
		mac := hmac.New(sha256.New, key)
		_, _ = mac.Write([]byte(canonical))
		expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(expected), []byte(signature)) {
			http.Error(w, "invalid event signature", http.StatusUnauthorized)
			return
		}
		var batch eventBatch
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&batch); err != nil || (batch.ContractVersion != contractVersion && batch.ContractVersion != "1.1") || len(batch.Events) == 0 || len(batch.Events) > 100 {
			http.Error(w, "invalid event batch", http.StatusBadRequest)
			return
		}
		if err := ensureSingleJSONValue(decoder); err != nil {
			http.Error(w, "invalid event batch", http.StatusBadRequest)
			return
		}
		inputs := make([]model.VFXFilesEventInput, 0, len(batch.Events))
		for _, raw := range batch.Events {
			input, err := parsePluginEvent(raw)
			if err != nil || contractVersionForEvent(raw) != batch.ContractVersion {
				http.Error(w, "invalid event payload", http.StatusBadRequest)
				return
			}
			inputs = append(inputs, input)
		}
		if batch.ContractVersion == "1.1" && !validShareBatchSemantics(batch.Events, inputs) {
			http.Error(w, "invalid event batch", http.StatusBadRequest)
			return
		}
		if strings.HasPrefix(inputs[0].EventType, "storage_authority.") {
			if len(inputs) != 1 {
				http.Error(w, "authority transition must be sent alone", http.StatusBadRequest)
				return
			}
			var transition model.VFXFilesEventResult
			transition, err := model.ApplyVFXFilesAuthorityTransition(db, keyID, nonce, inputs[0], current)
			if errors.Is(err, model.ErrVFXFilesEventConflict) {
				http.Error(w, "event replay or identity conflict", http.StatusConflict)
				return
			}
			if errors.Is(err, model.ErrVFXFilesAuthorityTransition) {
				http.Error(w, "authority transition cannot be proven", http.StatusConflict)
				return
			}
			if errors.Is(err, model.ErrVFXFilesAuthorityUnresolved) {
				w.Header().Set("Retry-After", "30")
				http.Error(w, "old-epoch delivery outcome remains unresolved", http.StatusServiceUnavailable)
				return
			}
			if err != nil {
				http.Error(w, "authority barrier is unavailable", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(struct {
				ContractVersion string                      `json:"contract_version"`
				Results         []model.VFXFilesEventResult `json:"results"`
			}{ContractVersion: contractVersion, Results: []model.VFXFilesEventResult{transition}})
			return
		}
		var results []model.VFXFilesEventResult
		results, err = model.AcceptVFXFilesEvents(db, keyID, nonce, inputs, current)
		if errors.Is(err, model.ErrVFXFilesEventConflict) {
			http.Error(w, "event replay or identity conflict", http.StatusConflict)
			return
		}
		if errors.Is(err, model.ErrVFXFilesActivationPending) {
			w.Header().Set("Retry-After", "2")
			http.Error(w, "activation state is synchronizing", http.StatusServiceUnavailable)
			return
		}
		if err != nil {
			http.Error(w, "event could not be durably accepted", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(struct {
			ContractVersion string                      `json:"contract_version"`
			Results         []model.VFXFilesEventResult `json:"results"`
		}{ContractVersion: contractVersion, Results: results})
	}
}

func validShareBatchSemantics(rawEvents []json.RawMessage, inputs []model.VFXFilesEventInput) bool {
	if len(rawEvents) == 0 || len(rawEvents) != len(inputs) {
		return false
	}
	productionID, epoch := inputs[0].ProductionID, inputs[0].ActivationEpoch
	seenTargets, seenRecipients := map[string]bool{}, map[string]bool{}
	type operationIdentity struct{ sourceType, entityID, taskID, taskTypeID, actorID, objectID, objectType, url string }
	operations := map[string]operationIdentity{}
	for i, raw := range rawEvents {
		if inputs[i].EventType != "file_share.created" || inputs[i].ProductionID != productionID || inputs[i].ActivationEpoch != epoch {
			return false
		}
		var event pluginEvent
		if json.Unmarshal(raw, &event) != nil || event.Source == nil || event.Recipient == nil || event.Share == nil {
			return false
		}
		if seenTargets[event.Share.ShareTargetID] || seenRecipients[event.Recipient.TaskID] {
			return false
		}
		seenTargets[event.Share.ShareTargetID], seenRecipients[event.Recipient.TaskID] = true, true
		identity := operationIdentity{event.Source.EntityType, event.Source.EntityID, event.Source.TaskID, event.Source.TaskTypeID, event.Share.SharedByID, event.Share.Object.ObjectID, event.Share.Object.ObjectType, event.Share.Object.URL}
		if previous, ok := operations[event.Share.ID]; ok && previous != identity {
			return false
		}
		operations[event.Share.ID] = identity
	}
	return true
}

func ensureSingleJSONValue(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON data")
	}
	return nil
}

func parsePluginEvent(raw []byte) (model.VFXFilesEventInput, error) {
	var event pluginEvent
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil {
		return model.VFXFilesEventInput{}, err
	}
	if err := ensureSingleJSONValue(decoder); err != nil {
		return model.VFXFilesEventInput{}, err
	}
	if (event.ContractVersion != contractVersion && event.ContractVersion != "1.1") || event.ProducerID != "vfx-files-plugin" || !validID(event.EventID) || !validID(event.ProductionID) || event.ActivationEpoch == 0 {
		return model.VFXFilesEventInput{}, errors.New("invalid event identity")
	}
	if event.EventType == "file_share.created" && event.ContractVersion != "1.1" || event.EventType != "file_share.created" && event.ContractVersion != contractVersion {
		return model.VFXFilesEventInput{}, errors.New("event type does not match contract version")
	}
	if _, err := time.Parse(time.RFC3339, event.OccurredAt); err != nil {
		return model.VFXFilesEventInput{}, errors.New("invalid event timestamp")
	}
	input := model.VFXFilesEventInput{ProducerID: event.ProducerID, EventID: event.EventID, EventType: event.EventType, ProductionID: event.ProductionID, ActivationEpoch: event.ActivationEpoch, Payload: append([]byte(nil), raw...)}
	switch event.EventType {
	case "file_delivery.created", "file_delivery.ready", "file_delivery.accepted", "file_delivery.needs_fix", "file_delivery.revised":
		if event.Entity == nil || event.Source == nil || event.Sender == nil || event.OutputPackage == nil || event.Delivery == nil || event.Folder != nil || event.SenderPersonID != nil || !validEntity(*event.Entity) || !validTask(*event.Source, true) || !validPerson(*event.Sender) || !validOutputPackage(*event.OutputPackage) || !validDelivery(*event.Delivery, event.EventType) {
			return model.VFXFilesEventInput{}, errors.New("incomplete file delivery event")
		}
		input.DeliveryID, input.DeliveryRevision = event.Delivery.ID, event.Delivery.Revision
		input.RecipientTaskID, input.RecipientTaskTypeID = event.Delivery.RecipientTaskID, event.Delivery.RecipientTaskTypeID
	case "output_package.created", "output_package.ready":
		if event.Source == nil || !validTask(*event.Source, false) || event.OutputPackage == nil || !validOutputPackage(*event.OutputPackage) || event.Delivery != nil || event.Entity != nil || event.Sender != nil || event.Folder != nil || (event.SenderPersonID != nil && !validID(*event.SenderPersonID)) {
			return model.VFXFilesEventInput{}, errors.New("incomplete output package event")
		}
	case "folder.provisioning.completed":
		if event.Folder == nil || !validFolder(*event.Folder) || event.Delivery != nil || event.Entity != nil || event.Source != nil || event.Sender != nil || event.SenderPersonID != nil || event.OutputPackage != nil {
			return model.VFXFilesEventInput{}, errors.New("incomplete folder event")
		}
	case "file_share.created":
		if event.ContractVersion != "1.1" || len(event.OccurredAt) > 40 || !validFileShareRequiredFields(raw) || event.Source == nil || !validShareSource(*event.Source) || event.Recipient == nil || !validShareRecipient(*event.Recipient) || event.Share == nil || !validShare(*event.Share) || event.Entity != nil || event.Sender != nil || event.SenderPersonID != nil || event.OutputPackage != nil || event.Delivery != nil || event.Folder != nil || event.EpisodeID != nil || event.EpisodeName != nil {
			return model.VFXFilesEventInput{}, errors.New("incomplete file share event")
		}
		input.RecipientTaskID, input.RecipientTaskTypeID = event.Recipient.TaskID, event.Recipient.TaskTypeID
		input.ShareID, input.ShareTargetID = event.Share.ID, event.Share.ShareTargetID
	case "storage_authority.activation_requested", "storage_authority.deactivation_requested":
		if event.ConfigRevision == 0 || event.PreviousActivationEpoch == 0 || event.PreviousActivationEpoch == ^uint64(0) || event.NewActivationEpoch != event.PreviousActivationEpoch+1 {
			return model.VFXFilesEventInput{}, errors.New("invalid authority transition epochs")
		}
		if event.EventType == "storage_authority.activation_requested" && event.ActivationEpoch != event.NewActivationEpoch ||
			event.EventType == "storage_authority.deactivation_requested" && event.ActivationEpoch != event.PreviousActivationEpoch {
			return model.VFXFilesEventInput{}, errors.New("invalid authority transition epoch")
		}
		input.ConfigRevision = event.ConfigRevision
		input.PreviousActivationEpoch = event.PreviousActivationEpoch
		input.NewActivationEpoch = event.NewActivationEpoch
	default:
		return model.VFXFilesEventInput{}, errors.New("unsupported event type")
	}
	return input, nil
}

func validFileShareRequiredFields(raw []byte) bool {
	var event map[string]json.RawMessage
	if json.Unmarshal(raw, &event) != nil || !onlyObjectKeys(raw, "contract_version", "producer_id", "event_id", "event_type", "occurred_at", "production_id", "activation_epoch", "source", "recipient", "share") ||
		!hasObjectKeys(event["source"], "entity_type", "entity_id", "task_id", "task_type_id") || !onlyObjectKeys(event["source"], "entity_type", "entity_id", "entity_name", "task_id", "task_type_id", "task_type_name") ||
		!hasObjectKeys(event["recipient"], "task_id", "task_type_id") || !onlyObjectKeys(event["recipient"], "task_id", "task_type_id", "task_type_name", "department_id", "department_name") || !hasObjectKeys(event["share"], "id", "share_target_id", "revision", "shared_by_id", "shared_at", "idempotency_key", "object", "metadata", "inventory", "access") {
		return false
	}
	var share map[string]json.RawMessage
	if json.Unmarshal(event["share"], &share) != nil || !hasObjectKeys(share["object"], "provider", "object_id", "object_type", "url") ||
		!hasObjectKeys(share["metadata"], "status", "observed_at", "name", "mime_type", "size_bytes", "modified_at", "sha256") ||
		!hasObjectKeys(share["inventory"], "status", "observed_at", "scope", "file_count", "observed_file_count_lower_bound", "frame_summary") ||
		!hasObjectKeys(share["access"], "status", "access_verified_at", "check_subject_type", "check_subject_id") {
		return false
	}
	var inventory map[string]json.RawMessage
	if json.Unmarshal(share["inventory"], &inventory) != nil {
		return false
	}
	if !bytes.Equal(bytes.TrimSpace(inventory["frame_summary"]), []byte("null")) {
		if !hasObjectKeys(inventory["frame_summary"], "status", "frame_start", "frame_end", "missing_count", "duplicate_count", "missing_samples") {
			return false
		}
	}
	return true
}

func hasObjectKeys(raw json.RawMessage, keys ...string) bool {
	var value map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil || value == nil {
		return false
	}
	for _, key := range keys {
		if _, ok := value[key]; !ok {
			return false
		}
	}
	return true
}

func onlyObjectKeys(raw json.RawMessage, keys ...string) bool {
	var value map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil || value == nil {
		return false
	}
	allowed := make(map[string]bool, len(keys))
	for _, key := range keys {
		allowed[key] = true
	}
	for key := range value {
		if !allowed[key] {
			return false
		}
	}
	return true
}

func contractVersionForEvent(raw []byte) string {
	var event struct {
		ContractVersion string `json:"contract_version"`
	}
	_ = json.Unmarshal(raw, &event)
	return event.ContractVersion
}

func validShareSource(source eventTask) bool {
	return source.ID == "" && validID(source.TaskID) && validID(source.TaskTypeID) && oneOf(source.EntityType, "shot", "asset") && validID(source.EntityID) && len([]rune(source.EntityName)) <= 200 && len([]rune(source.TaskTypeName)) <= 120 && !strings.ContainsRune(source.TaskTypeName, '\x00') && !strings.ContainsRune(source.EntityName, '\x00')
}

func validShareRecipient(recipient eventShareRecipient) bool {
	return validID(recipient.TaskID) && validID(recipient.TaskTypeID) && optionalText(recipient.TaskTypeName, 120) && optionalUUID(recipient.DepartmentID) && optionalText(recipient.DepartmentName, 120)
}

func validShare(share eventShare) bool {
	if !validID(share.ID) || !validID(share.ShareTargetID) || share.Revision != 1 || !validID(share.SharedByID) || len(share.SharedAt) > 40 || !validRFC3339(share.SharedAt) || len(share.IdempotencyKey) < 16 || len(share.IdempotencyKey) > 128 || !regexp.MustCompile(`^[A-Za-z0-9._~:-]+$`).MatchString(share.IdempotencyKey) {
		return false
	}
	obj := share.Object
	if obj.Provider != "google_drive" || !driveIDRE.MatchString(obj.ObjectID) || (obj.ObjectType != "file" && obj.ObjectType != "folder") || len(obj.URL) > 512 || !canonicalDriveURL(obj.URL, obj.ObjectID, obj.ObjectType) {
		return false
	}
	meta := share.Metadata
	if !oneOf(meta.Status, "complete", "partial", "unavailable") || len(meta.ObservedAt) > 40 || !validRFC3339(meta.ObservedAt) || !optionalText(meta.Name, 255) || !optionalText(meta.MIMEType, 127) || meta.SizeBytes != nil && *meta.SizeBytes < 0 || meta.ModifiedAt != nil && (len(*meta.ModifiedAt) > 40 || !validRFC3339(*meta.ModifiedAt)) || meta.SHA256 != nil && !regexpSHA256.MatchString(*meta.SHA256) {
		return false
	}
	if meta.Status == "unavailable" && (meta.Name != nil || meta.MIMEType != nil || meta.SizeBytes != nil || meta.ModifiedAt != nil || meta.SHA256 != nil) {
		return false
	}
	inv := share.Inventory
	if !oneOf(inv.Status, "complete", "partial", "unavailable") || len(inv.ObservedAt) > 40 || !validRFC3339(inv.ObservedAt) || !oneOf(inv.Scope, "object", "direct_children", "recursive") {
		return false
	}
	switch inv.Status {
	case "complete":
		if inv.FileCount == nil || inv.ObservedFileCountLowerBound != nil {
			return false
		}
	case "partial":
		if inv.FileCount != nil || inv.ObservedFileCountLowerBound == nil {
			return false
		}
	case "unavailable":
		if inv.FileCount != nil || inv.ObservedFileCountLowerBound != nil {
			return false
		}
	}
	if inv.FileCount != nil && (*inv.FileCount < 0 || *inv.FileCount > 1_000_000_000) || inv.ObservedFileCountLowerBound != nil && (*inv.ObservedFileCountLowerBound < 0 || *inv.ObservedFileCountLowerBound > 1_000_000_000) || !validFrameSummary(inv.FrameSummary) {
		return false
	}
	access := share.Access
	if !oneOf(access.Status, "verified_service_only", "denied", "unknown", "not_checked") || !oneOf(access.CheckSubjectType, "plugin_service_identity", "none") || optionalText(access.CheckSubjectID, 200) == false {
		return false
	}
	if access.Status == "verified_service_only" && (access.AccessVerifiedAt == nil || len(*access.AccessVerifiedAt) > 40 || !validRFC3339(*access.AccessVerifiedAt) || access.CheckSubjectType != "plugin_service_identity") {
		return false
	}
	if access.AccessVerifiedAt != nil && (len(*access.AccessVerifiedAt) > 40 || !validRFC3339(*access.AccessVerifiedAt)) {
		return false
	}
	return true
}

func canonicalDriveURL(raw, objectID, objectType string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "drive.google.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	want := "/file/d/" + objectID + "/view"
	if objectType == "folder" {
		want = "/drive/folders/" + objectID
	}
	return u.Path == want && raw == "https://drive.google.com"+want
}

func validRFC3339(value string) bool  { _, err := time.Parse(time.RFC3339, value); return err == nil }
func optionalUUID(value *string) bool { return value == nil || validID(*value) }
func optionalText(value *string, max int) bool {
	return value == nil || len([]rune(*value)) <= max && !strings.ContainsRune(*value, '\x00')
}
func oneOf(value string, values ...string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
func validFrameSummary(raw json.RawMessage) bool {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return true
	}
	var value struct {
		Status         string `json:"status"`
		FrameStart     *int   `json:"frame_start"`
		FrameEnd       *int   `json:"frame_end"`
		MissingCount   *int   `json:"missing_count"`
		DuplicateCount *int   `json:"duplicate_count"`
		MissingSamples []int  `json:"missing_samples"`
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&value) != nil || ensureSingleJSONValue(d) != nil || !oneOf(value.Status, "complete", "partial", "unavailable") || len(value.MissingSamples) > 20 {
		return false
	}
	for _, sample := range value.MissingSamples {
		if sample < -1_000_000_000 || sample > 1_000_000_000 {
			return false
		}
	}
	for _, p := range []*int{value.FrameStart, value.FrameEnd, value.MissingCount, value.DuplicateCount} {
		if p != nil && (*p < -1_000_000_000 || *p > 1_000_000_000) {
			return false
		}
	}
	if value.Status == "complete" {
		return value.FrameStart != nil && value.FrameEnd != nil && *value.FrameEnd >= *value.FrameStart && value.MissingCount != nil && *value.MissingCount >= 0 && value.DuplicateCount != nil && *value.DuplicateCount >= 0
	}
	if value.Status == "partial" {
		return value.MissingCount == nil && value.DuplicateCount == nil
	}
	return value.FrameStart == nil && value.FrameEnd == nil && value.MissingCount == nil && value.DuplicateCount == nil && len(value.MissingSamples) == 0
}

var regexpSHA256 = regexp.MustCompile(`^[a-f0-9]{64}$`)
var driveIDRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)

func boundedText(value string, max int) bool {
	return len([]rune(strings.TrimSpace(value))) > 0 && len([]rune(value)) <= max && !strings.ContainsRune(value, '\x00')
}

func validEntity(entity eventEntity) bool {
	return (entity.Type == "sequence" || entity.Type == "shot" || entity.Type == "asset") && validID(entity.ID) && boundedText(entity.Name, 240) && (entity.Code == nil || len([]rune(*entity.Code)) <= 120 && !strings.ContainsRune(*entity.Code, '\x00'))
}

func validTask(task eventTask, delivery bool) bool {
	if !validID(task.TaskTypeID) || len([]rune(task.TaskTypeName)) > 160 {
		return false
	}
	if delivery {
		return validID(task.ID) && task.TaskID == "" && boundedText(task.TaskTypeName, 160) && task.EntityType == "" && task.EntityID == "" && task.EntityName == ""
	}
	return validID(task.TaskID) && task.ID == "" && task.EntityType == "" && task.EntityID == "" && task.EntityName == ""
}

func validPerson(person eventPerson) bool {
	return validID(person.ID) && boundedText(person.DisplayName, 160)
}

func validOutputPackage(pkg eventOutputPackage) bool {
	if !validID(pkg.ID) || pkg.Version < 1 || !boundedText(pkg.Classification, 100) || !regexpSHA256.MatchString(pkg.ManifestSHA256) || len(pkg.Objects) > 100 {
		return false
	}
	if pkg.ManifestRef != nil && (len([]rune(*pkg.ManifestRef)) > 200 || !regexp.MustCompile(`^manifest:[0-9a-fA-F-]{36}$`).MatchString(*pkg.ManifestRef)) {
		return false
	}
	for _, object := range pkg.Objects {
		if !driveIDRE.MatchString(object.DriveFileID) || !boundedText(object.Classification, 100) || object.SizeBytes < 0 || !regexpSHA256.MatchString(object.SHA256) {
			return false
		}
	}
	if pkg.FrameRange != nil {
		f := pkg.FrameRange
		if f.Step < 1 || f.RateNum < 1 || f.RateDen < 1 || f.End < f.Start {
			return false
		}
	}
	return true
}

func validDelivery(delivery eventDelivery, eventType string) bool {
	wantStatus := strings.TrimPrefix(eventType, "file_delivery.")
	if !validID(delivery.ID) || delivery.Revision == 0 || delivery.Status != wantStatus || !validID(delivery.RecipientTaskID) || !validID(delivery.RecipientTaskTypeID) || !boundedText(delivery.RecipientTaskTypeName, 160) {
		return false
	}
	return delivery.FeedbackCode == nil || len([]rune(*delivery.FeedbackCode)) <= 80 && !strings.ContainsRune(*delivery.FeedbackCode, '\x00')
}

func validFolder(folder eventFolder) bool {
	allowedType := map[string]bool{"production": true, "episode": true, "sequence": true, "shot": true, "asset": true, "task": true}
	return allowedType[folder.ObjectType] && validID(folder.ObjectID) && driveIDRE.MatchString(folder.DriveFileID) && folder.BindingState == "ready"
}
